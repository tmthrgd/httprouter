// Copyright 2013 Julien Schmidt. All rights reserved.
// Based on the path package, Copyright 2009 The Go Authors.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

package httprouter

import (
	"context"
)

type contextKey int

const (
	paramKey = contextKey(0)
	panicKey = contextKey(1)
)

// GetParams returns the Param-slice associated with a context.Context
// if there is one, otherwise it returns nil.
func GetParams(ctx context.Context) Params {
	ps, _ := ctx.Value(paramKey).(Params)
	return ps
}

// GetValue is short-hand for GetParams(ctx).ByName(name).
func GetValue(ctx context.Context, name string) string {
	ps, _ := ctx.Value(paramKey).(Params)
	return ps.ByName(name)
}

// GetPanic returns the recovered panic value associated with a
// context.Context.
func GetPanic(ctx context.Context) interface{} {
	return ctx.Value(panicKey)
}
