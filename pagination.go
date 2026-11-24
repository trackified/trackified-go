package trackified

import (
	"context"
	"iter"
	"net/url"
	"strconv"
)

// ListParams are the paging parameters shared by every list call.
type ListParams struct {
	Limit  int // 1..100, server default 50
	Offset int
}

func (p ListParams) query() url.Values {
	q := url.Values{}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	return q
}

// Iter walks every item of a list across pages, fetching the next page on demand.
//
//	it := client.Trackings.ListAutoPaging(ctx, nil)
//	for it.Next() {
//		t := it.Current()
//	}
//	if err := it.Err(); err != nil { ... }
//
// With Go 1.23+ prefer the range form: for t, err := range it.All() { ... }.
type Iter[T any] struct {
	ctx    context.Context
	fetch  func(ctx context.Context, offset int) (*List[T], error)
	offset int
	page   []T
	i      int
	cur    T
	err    error
	done   bool
}

func newIter[T any](ctx context.Context, offset int, fetch func(context.Context, int) (*List[T], error)) *Iter[T] {
	return &Iter[T]{ctx: ctx, fetch: fetch, offset: offset}
}

// Next advances to the next item, fetching a page when needed. It returns false at the end or on error.
func (it *Iter[T]) Next() bool {
	for it.i >= len(it.page) {
		if it.done || it.err != nil {
			return false
		}
		l, err := it.fetch(it.ctx, it.offset)
		if err != nil {
			it.err = err
			return false
		}
		it.page, it.i = l.Data, 0
		it.offset += len(l.Data)
		if len(l.Data) == 0 || it.offset >= l.Pagination.Total {
			it.done = true
		}
	}
	it.cur = it.page[it.i]
	it.i++
	return true
}

// Current returns the item Next moved to.
func (it *Iter[T]) Current() T { return it.cur }

// Err returns the error that stopped iteration, if any.
func (it *Iter[T]) Err() error { return it.err }

// All adapts the iterator to a range-over-func sequence. An error is yielded once, as the last pair.
func (it *Iter[T]) All() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for it.Next() {
			if !yield(it.Current(), nil) {
				return
			}
		}
		if it.err != nil {
			var zero T
			yield(zero, it.err)
		}
	}
}
