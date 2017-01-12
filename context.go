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

type paramName string

type paramsContext struct {
	context.Context
	values Params
}

func (c paramsContext) Value(key interface{}) interface{} {
	if key == paramKey {
		return c.values
	}
	if key, ok := key.(paramName); ok {
		return c.values.ByName(string(key))
	}
	return c.Context.Value(key)
}

// GetParams returns the Param-slice associated with a context.Context
// if there is one, otherwise it returns nil.
func GetParams(ctx context.Context) Params {
	ps, _ := ctx.Value(paramKey).(Params)
	return ps
}

// GetParams returns the value of the first Param associated with a
// context.Context which key matches the given name.
// If no matching Param is found, an empty string is returned.
func GetValue(ctx context.Context, name string) string {
	val, _ := ctx.Value(paramName(name)).(string)
	return val
}

// GetPanic returns the recovered panic value associated with a
// context.Context.
func GetPanic(ctx context.Context) interface{} {
	return ctx.Value(panicKey)
}
