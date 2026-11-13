package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

// LocalePhase represents the execution lifecycle state of one target locale.
type LocalePhase string

const (
	PhaseQueued   LocalePhase = "queued"
	PhaseRunning  LocalePhase = "running"
	PhaseDone     LocalePhase = "done"
	PhasePartial  LocalePhase = "partial"
	PhaseFailed   LocalePhase = "failed"
	PhaseCanceled LocalePhase = "canceled"
)

// LocaleStatus tracks real-time concurrent translation status for one target locale.
type LocaleStatus struct {
	LocaleID       string
	LocaleName     string
	Phase          LocalePhase
	ActiveProvider string
	Total          int
	Completed      int
	CacheHits      int
	Errors         []string
}

// LogEvent records a structured diagnostic event during a translation dispatch run.
type LogEvent struct {
	Time       time.Time
	Level      string
	ProviderID string
	LocaleID   string
	Message    string
}

// TranslatedCell represents a single translated cell produced during dispatch.
type TranslatedCell struct {
	LocaleID   string
	Key        domain.EntryKey
	Text       string
	ProviderID string
}

// DispatchOptions configures a multi-locale, multi-provider concurrent translation run.
type DispatchOptions struct {
	Registry    *Registry
	Cache       *Cache
	ProviderIDs []string
	Locales     []locale.LocaleSpec
	Tasks       map[string][]SourceItem
	Concurrency int
	BypassCache bool
	OnUpdate    func(statuses map[string]LocaleStatus, newCells []TranslatedCell, newEvents []LogEvent)
}

// DispatchResult holds the final statuses, translated cells, log events, and duration.
type DispatchResult struct {
	Statuses map[string]LocaleStatus
	Cells    []TranslatedCell
	Events   []LogEvent
	Duration time.Duration
}

// RunConcurrent fans out semaphore-bounded per-locale workers running ProviderIDs in waterfall order.
func RunConcurrent(ctx context.Context, opts DispatchOptions) *DispatchResult {
	start := time.Now()
	reg := opts.Registry
	if reg == nil {
		reg = DefaultRegistry()
	}
	cache := opts.Cache
	if cache == nil {
		cache = GlobalCache()
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	res := &DispatchResult{
		Statuses: make(map[string]LocaleStatus, len(opts.Locales)),
	}
	for _, loc := range opts.Locales {
		items := opts.Tasks[loc.ID]
		phase := PhaseQueued
		if len(items) == 0 {
			phase = PhaseDone
		}
		res.Statuses[loc.ID] = LocaleStatus{
			LocaleID:   loc.ID,
			LocaleName: loc.Name,
			Phase:      phase,
			Total:      len(items),
		}
	}

	var mu sync.Mutex
	emit := func(localeID string, updateFn func(st *LocaleStatus), newCells []TranslatedCell, newEvents []LogEvent) {
		mu.Lock()
		if updateFn != nil {
			st := res.Statuses[localeID]
			updateFn(&st)
			res.Statuses[localeID] = st
		}
		if len(newCells) > 0 {
			res.Cells = append(res.Cells, newCells...)
		}
		if len(newEvents) > 0 {
			res.Events = append(res.Events, newEvents...)
		}

		var statusSnap map[string]LocaleStatus
		if opts.OnUpdate != nil {
			statusSnap = make(map[string]LocaleStatus, len(res.Statuses))
			for k, v := range res.Statuses {
				v.Errors = slices.Clone(v.Errors)
				statusSnap[k] = v
			}
		}
		mu.Unlock()

		if opts.OnUpdate != nil {
			opts.OnUpdate(statusSnap, slices.Clone(newCells), slices.Clone(newEvents))
		}
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, loc := range opts.Locales {
		items := opts.Tasks[loc.ID]
		if len(items) == 0 {
			continue
		}

		wg.Add(1)
		go func(loc locale.LocaleSpec, items []SourceItem) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				emit(loc.ID, func(st *LocaleStatus) {
					st.Phase = PhaseCanceled
				}, nil, []LogEvent{{
					Time:     time.Now(),
					Level:    "warn",
					LocaleID: loc.ID,
					Message:  "canceled while queued",
				}})
				return
			}

			emit(loc.ID, func(st *LocaleStatus) {
				st.Phase = PhaseRunning
			}, nil, nil)

			remaining := slices.Clone(items)
			completed := 0
			cacheHits := 0
			var errs []string

			for _, pid := range opts.ProviderIDs {
				if len(remaining) == 0 {
					break
				}
				if ctx.Err() != nil {
					break
				}

				p, ok := reg.Get(pid)
				if !ok {
					ev := LogEvent{
						Time:       time.Now(),
						Level:      "warn",
						ProviderID: pid,
						LocaleID:   loc.ID,
						Message:    fmt.Sprintf("provider %q not registered, skipping", pid),
					}
					emit(loc.ID, nil, nil, []LogEvent{ev})
					continue
				}

				emit(loc.ID, func(st *LocaleStatus) {
					st.ActiveProvider = pid
				}, nil, nil)

				engineCode := locale.ResolveEngineCode(loc, pid)

				if p.Kind() == KindLocal {
					outMap, err := p.Translate(ctx, Request{
						TargetLocale: loc,
						EngineCode:   engineCode,
						Items:        remaining,
					})
					if err != nil {
						errs = append(errs, fmt.Sprintf("%s: %v", pid, err))
						ev := LogEvent{
							Time:       time.Now(),
							Level:      "error",
							ProviderID: pid,
							LocaleID:   loc.ID,
							Message:    err.Error(),
						}
						emit(loc.ID, func(st *LocaleStatus) {
							st.Errors = append(st.Errors, fmt.Sprintf("%s: %v", pid, err))
						}, nil, []LogEvent{ev})
						continue
					}

					var newCells []TranslatedCell
					var nextRemaining []SourceItem
					for _, item := range remaining {
						if txt := outMap[item.ID]; txt != "" {
							newCells = append(newCells, TranslatedCell{
								LocaleID:   loc.ID,
								Key:        item.Key,
								Text:       txt,
								ProviderID: pid,
							})
							completed++
						} else {
							nextRemaining = append(nextRemaining, item)
						}
					}
					remaining = nextRemaining

					if len(newCells) > 0 {
						ev := LogEvent{
							Time:       time.Now(),
							Level:      "info",
							ProviderID: pid,
							LocaleID:   loc.ID,
							Message:    fmt.Sprintf("matched %d entries", len(newCells)),
						}
						emit(loc.ID, func(st *LocaleStatus) {
							st.Completed = completed
						}, newCells, []LogEvent{ev})
					}
					continue
				}

				// KindEngine: check cache first unless BypassCache is set.
				var toTranslate []SourceItem
				var cacheCells []TranslatedCell
				if !opts.BypassCache {
					for _, item := range remaining {
						cacheSrc := item.Key.Source
						if cacheSrc == "" {
							cacheSrc = item.RefText
						}
						if cached, hit := cache.Get(pid, loc.ID, cacheSrc, item.Key.Context); hit {
							cacheCells = append(cacheCells, TranslatedCell{
								LocaleID:   loc.ID,
								Key:        item.Key,
								Text:       cached,
								ProviderID: domain.OriginCache,
							})
							completed++
							cacheHits++
						} else {
							toTranslate = append(toTranslate, item)
						}
					}
				} else {
					toTranslate = remaining
				}

				if len(cacheCells) > 0 {
					ev := LogEvent{
						Time:       time.Now(),
						Level:      "info",
						ProviderID: pid,
						LocaleID:   loc.ID,
						Message:    fmt.Sprintf("cache hit %d entries", len(cacheCells)),
					}
					emit(loc.ID, func(st *LocaleStatus) {
						st.Completed = completed
						st.CacheHits = cacheHits
					}, cacheCells, []LogEvent{ev})
				}

				if len(toTranslate) == 0 {
					remaining = nil
					break
				}

				// Mask placeholders (splitting numerus forms cleanly when needed).
				type itemPlan struct {
					orig   SourceItem
					subIDs []string
					masked []MaskedText
				}
				plans := make([]itemPlan, 0, len(toTranslate))
				maskedItems := make([]SourceItem, 0, len(toTranslate))

				for _, item := range toTranslate {
					baseSrc := item.Key.Source
					if baseSrc == "" {
						baseSrc = item.RefText
					}
					if item.Numerus {
						pluralSrc := baseSrc
						if !strings.Contains(pluralSrc, domain.PluralSeparator) && strings.Contains(item.RefText, domain.PluralSeparator) {
							pluralSrc = item.RefText
						}
						forms := domain.SplitNumerus(pluralSrc)
						if len(forms) > 1 {
							pl := itemPlan{
								orig:   item,
								subIDs: make([]string, len(forms)),
								masked: make([]MaskedText, len(forms)),
							}
							for fi, form := range forms {
								subID := fmt.Sprintf("%s#%d", item.ID, fi)
								m := MaskPlaceholders(form)
								pl.subIDs[fi] = subID
								pl.masked[fi] = m
								mk := item.Key
								mk.Source = m.Masked
								maskedItems = append(maskedItems, SourceItem{
									ID:        subID,
									Key:       mk,
									RefLocale: item.RefLocale,
									Numerus:   true,
								})
							}
							plans = append(plans, pl)
							continue
						}
					}
					m := MaskPlaceholders(baseSrc)
					plans = append(plans, itemPlan{
						orig:   item,
						subIDs: []string{item.ID},
						masked: []MaskedText{m},
					})
					mk := item.Key
					mk.Source = m.Masked
					maskedItems = append(maskedItems, SourceItem{
						ID:        item.ID,
						Key:       mk,
						RefLocale: item.RefLocale,
						RefText:   item.RefText,
						Numerus:   item.Numerus,
					})
				}

				outMap, err := p.Translate(ctx, Request{
					TargetLocale: loc,
					EngineCode:   engineCode,
					Items:        maskedItems,
				})
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", pid, err))
					ev := LogEvent{
						Time:       time.Now(),
						Level:      "error",
						ProviderID: pid,
						LocaleID:   loc.ID,
						Message:    err.Error(),
					}
					emit(loc.ID, func(st *LocaleStatus) {
						st.Errors = append(st.Errors, fmt.Sprintf("%s: %v", pid, err))
					}, nil, []LogEvent{ev})
					remaining = toTranslate
					continue
				}

				var newCells []TranslatedCell
				var nextRemaining []SourceItem
				for _, pl := range plans {
					if len(pl.subIDs) == 1 {
						raw := outMap[pl.subIDs[0]]
						restored := RestorePlaceholders(raw, pl.masked[0])
						if restored != "" {
							cacheSrc := pl.orig.Key.Source
							if cacheSrc == "" {
								cacheSrc = pl.orig.RefText
							}
							cache.Put(pid, loc.ID, cacheSrc, pl.orig.Key.Context, restored)
							newCells = append(newCells, TranslatedCell{
								LocaleID:   loc.ID,
								Key:        pl.orig.Key,
								Text:       restored,
								ProviderID: pid,
							})
							completed++
						} else {
							nextRemaining = append(nextRemaining, pl.orig)
						}
					} else {
						forms := make([]string, len(pl.subIDs))
						allOK := true
						for fi, subID := range pl.subIDs {
							restored := RestorePlaceholders(outMap[subID], pl.masked[fi])
							if restored == "" {
								allOK = false
								break
							}
							forms[fi] = restored
						}
						if allOK {
							joined := domain.JoinNumerus(forms)
							cacheSrc := pl.orig.Key.Source
							if cacheSrc == "" {
								cacheSrc = pl.orig.RefText
							}
							cache.Put(pid, loc.ID, cacheSrc, pl.orig.Key.Context, joined)
							newCells = append(newCells, TranslatedCell{
								LocaleID:   loc.ID,
								Key:        pl.orig.Key,
								Text:       joined,
								ProviderID: pid,
							})
							completed++
						} else {
							nextRemaining = append(nextRemaining, pl.orig)
						}
					}
				}
				remaining = nextRemaining

				if len(newCells) > 0 {
					ev := LogEvent{
						Time:       time.Now(),
						Level:      "info",
						ProviderID: pid,
						LocaleID:   loc.ID,
						Message:    fmt.Sprintf("translated %d entries", len(newCells)),
					}
					emit(loc.ID, func(st *LocaleStatus) {
						st.Completed = completed
					}, newCells, []LogEvent{ev})
				}
			}

			finalPhase := PhaseDone
			if ctx.Err() != nil {
				finalPhase = PhaseCanceled
			} else if completed == len(items) {
				finalPhase = PhaseDone
			} else if completed > 0 {
				finalPhase = PhasePartial
			} else if len(errs) > 0 {
				finalPhase = PhaseFailed
			}

			emit(loc.ID, func(st *LocaleStatus) {
				st.Phase = finalPhase
				st.ActiveProvider = ""
				st.Completed = completed
				st.CacheHits = cacheHits
			}, nil, nil)
		}(loc, items)
	}

	wg.Wait()
	_ = cache.Save()
	res.Duration = time.Since(start)
	return res
}
