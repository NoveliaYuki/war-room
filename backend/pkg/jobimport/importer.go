// Package jobimport turns a job posting link into a draft for the new-process
// form. Everything it fetches is untrusted, and it never saves anything.
package jobimport

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"time"
)

const (
	cacheTTL        = 15 * time.Minute
	maxCacheEntries = 512
	rateWindow      = 10 * time.Minute
	rateLimit       = 30
	maxConcurrent   = 8
	readTimeout     = 15 * time.Second
)

// reader extracts postings from the network, without caching or limits.
type reader struct {
	fetcher *fetcher
	api     endpoints
}

// read tries a board API, then the page's own data, in that order.
func (r *reader) read(ctx context.Context, target *url.URL) (*Posting, error) {
	if board, ok := boardFromURL(target); ok {
		return r.readBoard(ctx, board)
	}
	res, err := r.fetcher.get(ctx, target, acceptPage, maxPageBytes)
	if err != nil {
		return nil, err
	}
	if board, ok := boardFromURL(res.finalURL); ok {
		return r.readBoard(ctx, board)
	}
	if board, ok := embeddedBoard(res.finalURL, res.body); ok {
		return r.readBoard(ctx, board)
	}
	if posting := postingFromJSONLD(res.body); posting != nil {
		return posting, nil
	}
	return nil, newError(CodeUnsupported, "No job details were found on that page.")
}

type cacheEntry struct {
	posting Posting
	expires time.Time
}

type rateEntry struct {
	count int
	reset time.Time
}

type flight struct {
	done    chan struct{}
	posting *Posting
	err     error
}

// Importer adds caching, per-user limits, and a global concurrency cap. State
// is in memory, which holds while one backend instance serves the app.
type Importer struct {
	reader *reader
	now    func() time.Time
	slots  chan struct{}

	mu       sync.Mutex
	cache    map[string]cacheEntry
	rates    map[string]rateEntry
	inflight map[string]*flight
}

// New creates an importer that only reaches public internet addresses.
func New() *Importer {
	return newImporter(&reader{fetcher: newFetcher(isPublicAddr, "443"), api: liveEndpoints})
}

func newImporter(r *reader) *Importer {
	return &Importer{
		reader: r, now: time.Now, slots: make(chan struct{}, maxConcurrent),
		cache: map[string]cacheEntry{}, rates: map[string]rateEntry{}, inflight: map[string]*flight{},
	}
}

// Preview returns a draft for rawURL. Cached postings do not count against the
// user's limit, and identical concurrent imports share one upstream read.
func (i *Importer) Preview(ctx context.Context, userID, rawURL string) (*Posting, error) {
	target, err := Canonicalize(rawURL)
	if err != nil {
		return nil, err
	}
	if isPasteOnly(target.Hostname()) {
		return nil, newError(CodePasteRequired, "This site does not allow automatic imports.")
	}
	if err := i.reader.fetcher.validateTarget(target); err != nil {
		return nil, err
	}
	key := target.String()
	if posting := i.cached(key); posting != nil {
		return posting, nil
	}
	if !i.allow(userID) {
		return nil, newError(CodeRateLimited, "Too many imports. Try again in a few minutes.")
	}
	return i.shared(ctx, key, target)
}

func (i *Importer) cached(key string) *Posting {
	i.mu.Lock()
	defer i.mu.Unlock()
	entry, ok := i.cache[key]
	if !ok || i.now().After(entry.expires) {
		delete(i.cache, key)
		return nil
	}
	return clonePosting(entry.posting)
}

func clonePosting(posting Posting) *Posting {
	posting.Warnings = append([]string{}, posting.Warnings...)
	return &posting
}

func (i *Importer) allow(userID string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	now := i.now()
	entry := i.rates[userID]
	if now.After(entry.reset) {
		i.pruneRates(now)
		entry = rateEntry{reset: now.Add(rateWindow)}
	}
	if entry.count >= rateLimit {
		return false
	}
	entry.count++
	i.rates[userID] = entry
	return true
}

func (i *Importer) pruneRates(now time.Time) {
	for user, entry := range i.rates {
		if now.After(entry.reset) {
			delete(i.rates, user)
		}
	}
}

// shared joins an identical read already in progress, or starts one that
// outlives a single caller's disconnect so the others still get a result.
func (i *Importer) shared(ctx context.Context, key string, target *url.URL) (*Posting, error) {
	i.mu.Lock()
	call, joined := i.inflight[key]
	if !joined {
		call = &flight{done: make(chan struct{})}
		i.inflight[key] = call
		go i.run(key, target, call)
	}
	i.mu.Unlock()
	select {
	case <-call.done:
		if call.err != nil {
			return nil, call.err
		}
		return clonePosting(*call.posting), nil
	case <-ctx.Done():
		return nil, newError(CodeTimeout, "The import was cancelled.")
	}
}

func (i *Importer) run(key string, target *url.URL, call *flight) {
	defer close(call.done)
	call.posting, call.err = i.readWithSlot(target)
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.inflight, key)
	if call.err == nil {
		i.store(key, *call.posting)
	}
}

// readWithSlot runs outside the HTTP server's panic recovery, so a parser panic
// on hostile content must become an ordinary failure here.
func (i *Importer) readWithSlot(target *url.URL) (posting *Posting, err error) {
	defer func() {
		if recover() != nil {
			posting, err = nil, newError(CodeUnsupported, "No job details were found on that page.")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	return i.acquireAndRead(ctx, target)
}

func (i *Importer) acquireAndRead(ctx context.Context, target *url.URL) (*Posting, error) {
	select {
	case i.slots <- struct{}{}:
		defer func() { <-i.slots }()
	case <-ctx.Done():
		return nil, newError(CodeBusy, "Imports are busy right now. Try again shortly.")
	}
	posting, err := i.reader.read(ctx, target)
	if err == nil && (posting.PositionTitle == "" || posting.CompanyOverview == "") {
		err = newError(CodeUnsupported, "No job details were found on that page.")
	}
	if err != nil {
		logFailure(target, err)
		return nil, err
	}
	posting.JobPostURL = target.String()
	if posting.Warnings == nil {
		posting.Warnings = []string{}
	}
	return posting, nil
}

// logFailure records only the host: posting URLs can carry personal tokens.
func logFailure(target *url.URL, err error) {
	var importError *Error
	if errors.As(err, &importError) {
		slog.Warn("job import failed", "host", target.Hostname(), "import_code", importError.Code, "detail", importError.Message)
	}
}

func (i *Importer) store(key string, posting Posting) {
	now := i.now()
	if len(i.cache) >= maxCacheEntries {
		for candidate, entry := range i.cache {
			if now.After(entry.expires) {
				delete(i.cache, candidate)
			}
		}
	}
	if len(i.cache) >= maxCacheEntries {
		i.cache = map[string]cacheEntry{}
	}
	i.cache[key] = cacheEntry{posting: posting, expires: now.Add(cacheTTL)}
}
