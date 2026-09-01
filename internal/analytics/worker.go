package analytics

import (
    "context"
    "log"
    "sync"

    "url-shortener/internal/store"
)

type Event struct {
    Code      string
    IP        string
    UserAgent string
    Referer   string
}

type Worker struct {
    store *store.Store
    queue chan Event
    done  chan struct{}
    once  sync.Once
    wg    sync.WaitGroup
}

func NewWorker(st *store.Store, size int) *Worker {
    return &Worker{store: st, queue: make(chan Event, size), done: make(chan struct{})}
}

func (w *Worker) Start(ctx context.Context) {
    w.wg.Add(1)
    go func() {
        defer w.wg.Done()
        for {
            select {
            case e := <-w.queue:
                if err := w.store.RecordClick(ctx, e.Code, e.IP, e.UserAgent, e.Referer); err != nil {
                    log.Println("analytics:", err)
                }
            case <-ctx.Done():
                return
            case <-w.done:
                for {
                    select {
                    case e := <-w.queue:
                        if err := w.store.RecordClick(context.Background(), e.Code, e.IP, e.UserAgent, e.Referer); err != nil {
                            log.Println("analytics:", err)
                        }
                    default:
                        return
                    }
                }
            }
        }
    }()
}

func (w *Worker) Enqueue(e Event) {
    select {
    case w.queue <- e:
    default:
        // Drop only when the bounded queue is full; never block redirects.
    }
}

func (w *Worker) Stop() {
    w.once.Do(func() { close(w.done) })
    w.wg.Wait()
}
