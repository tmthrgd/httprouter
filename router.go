// Copyright 2013 Julien Schmidt. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

// Package httprouter is a trie based high performance HTTP request router.
//
// A trivial example is:
//
//  package main
//
//  import (
//      "fmt"
//      "github.com/tmthrgd/httprouter"
//      "net/http"
//      "log"
//  )
//
//  var Index = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//      fmt.Fprint(w, "Welcome!\n")
//  })
//
//  var Hello = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//      name := httprouter.GetValue(r.Context(), "name")
//      fmt.Fprintf(w, "hello, %s!\n", name)
//  })
//
//  func main() {
//      router := httprouter.New()
//      router.Get("/", Index)
//      router.Get("/hello/:name", Hello)
//
//      log.Fatal(http.ListenAndServe(":8080", router))
//  }
//
// The router matches incoming requests by the request method and the path.
// If a handle is registered for this path and method, the router delegates the
// request to that function.
// For the methods GET, POST, PUT, PATCH and DELETE shortcut functions exist to
// register handles, for all other methods router.Handle can be used.
//
// The registered path, against which the router matches incoming requests, can
// contain two types of parameters:
//  Syntax    Type
//  :name     named parameter
//  *name     catch-all parameter
//
// Named parameters are dynamic path segments. They match anything until the
// next '/' or the path end:
//  Path: /blog/:category/:post
//
//  Requests:
//   /blog/go/request-routers            match: category="go", post="request-routers"
//   /blog/go/request-routers/           no match, but the router would redirect
//   /blog/go/                           no match
//   /blog/go/request-routers/comments   no match
//
// Catch-all parameters match anything until the path end, including the
// directory index (the '/' before the catch-all). Since they match anything
// until the end, catch-all parameters must always be the final path element.
//  Path: /files/*filepath
//
//  Requests:
//   /files/                             match: filepath="/"
//   /files/LICENSE                      match: filepath="/LICENSE"
//   /files/templates/article.html       match: filepath="/templates/article.html"
//   /files                              no match, but the router would redirect
//
// The value of parameters is saved as a slice of the Param struct, consisting
// each of a key and a value. The slice is accessable with the GetParams method.
//
// There are two ways to retrieve the value of a parameter:
//  // by the name of the parameter
//  user := ps.ByName("user") // defined by :user or *user
//
//  // by the index of the parameter. This way you can also get the name (key)
//  thirdKey   := ps[2].Key   // the name of the 3rd parameter
//  thirdValue := ps[2].Value // the value of the 3rd parameter
package httprouter

import (
	"context"
	"fmt"
	"net/http"
)

// contextKey is a value for use with context.WithValue. It's used as
// a pointer so it fits in an interface{} without allocation.
type contextKey struct{ name string }

func (k *contextKey) String() string { return "httprouter context value " + k.name }

var (
	paramKey = &contextKey{"param"}
	panicKey = &contextKey{"panic"}
)

// Param is a single URL parameter, consisting of a key and a value.
type Param struct {
	Key   string
	Value string
}

// Params is a Param-slice, as returned by the router.
// The slice is ordered, the first URL parameter is also the first slice value.
// It is therefore safe to read values by the index.
type Params []Param

// ByName returns the value of the first Param which key matches the given name.
// If no matching Param is found, an empty string is returned.
func (ps Params) ByName(name string) string {
	for i := range ps {
		if ps[i].Key == name {
			return ps[i].Value
		}
	}
	return ""
}

// GetParams returns the Param-slice associated with a context.Context
// if there is one, otherwise it returns nil.
func GetParams(ctx context.Context) Params {
	if ps := ctx.Value(paramKey); ps != nil {
		return *ps.(*Params)
	}
	return nil
}

// GetValue is short-hand for GetParams(ctx).ByName(name).
func GetValue(ctx context.Context, name string) string {
	return GetParams(ctx).ByName(name)
}

type paramsContext struct {
	context.Context
	ps Params
}

func (c *paramsContext) String() string {
	return fmt.Sprintf("%v.WithValue(%#v, %#v)", c.Context, paramKey, &c.ps)
}

func (c *paramsContext) Value(key interface{}) interface{} {
	if key == paramKey {
		return &c.ps
	}
	return c.Context.Value(key)
}

// GetPanic returns the recovered panic value associated with a
// context.Context.
func GetPanic(ctx context.Context) interface{} {
	return ctx.Value(panicKey)
}

// PathHandler wraps a http.Handler and replaces the request URLs path with
// the value of the filepath param. It must be used with a path that ends
// with "/*filepath".
func PathHandler(h http.Handler) http.Handler {
	return &pathHandler{h}
}

type pathHandler struct {
	http.Handler
}

func (h *pathHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	u := *req.URL
	u.Path = GetValue(req.Context(), "filepath")

	r := *req
	r.URL = &u

	h.Handler.ServeHTTP(w, &r)
}

// Router is a http.Handler which can be used to dispatch requests to different
// handler functions via configurable routes
type Router struct {
	trees map[string]*node

	// Enables automatic redirection if the current route can't be matched but a
	// handler for the path with (without) the trailing slash exists.
	// For example if /foo/ is requested but a route only exists for /foo, the
	// client is redirected to /foo with http status code 301 for GET requests
	// and 307 for all other request methods.
	RedirectTrailingSlash bool

	// If enabled, the router tries to fix the current request path, if no
	// handle is registered for it.
	// First superfluous path elements like ../ or // are removed.
	// Afterwards the router does a case-insensitive lookup of the cleaned path.
	// If a handle can be found for this route, the router makes a redirection
	// to the corrected path with status code 301 for GET requests and 307 for
	// all other request methods.
	// For example /FOO and /..//Foo could be redirected to /foo.
	// RedirectTrailingSlash is independent of this option.
	RedirectFixedPath bool

	// If enabled, the router checks if another method is allowed for the
	// current route, if the current request can not be routed.
	// If this is the case, the request is answered with 'Method Not Allowed'
	// and HTTP status code 405.
	// If no other Method is allowed, the request is delegated to the NotFound
	// handler.
	HandleMethodNotAllowed bool

	// If enabled, the router automatically replies to OPTIONS requests.
	// Custom OPTIONS handlers take priority over automatic replies.
	HandleOptions bool

	// Configurable http.Handler which is called when no matching route is
	// found. If it is not set, http.NotFound is used.
	NotFound http.Handler

	// Configurable http.Handler which is called when a request
	// cannot be routed and HandleMethodNotAllowed is true.
	// If it is not set, http.Error with http.StatusMethodNotAllowed is used.
	// The "Allow" header with allowed request methods is set before the handler
	// is called.
	MethodNotAllowed http.Handler

	// Function to handle panics recovered from http handlers.
	// It should be used to generate a error page and return the http error code
	// 500 (Internal Server Error).
	// The handler can be used to keep your server from crashing because of
	// unrecovered panics.
	PanicHandler http.Handler
}

// Make sure the Router conforms with the http.Handler interface
var _ http.Handler = New()

// New returns a new initialized Router.
// Path auto-correction, including trailing slashes, is enabled by default.
func New() *Router {
	return &Router{
		RedirectTrailingSlash:  true,
		RedirectFixedPath:      true,
		HandleMethodNotAllowed: true,
		HandleOptions:          true,
	}
}

// Get is a shortcut for router.Handle(http.MethodGet, path, handle)
func (r *Router) Get(path string, handle http.Handler) {
	r.Handle(http.MethodGet, path, handle)
}

// Head is a shortcut for router.Handle(http.MethodHead, path, handle)
func (r *Router) Head(path string, handle http.Handler) {
	r.Handle(http.MethodHead, path, handle)
}

// Options is a shortcut for router.Handle(http.MethodOptions, path, handle)
func (r *Router) Options(path string, handle http.Handler) {
	r.Handle(http.MethodOptions, path, handle)
}

// Post is a shortcut for router.Handle(http.MethodPost, path, handle)
func (r *Router) Post(path string, handle http.Handler) {
	r.Handle(http.MethodPost, path, handle)
}

// Put is a shortcut for router.Handle(http.MethodPut, path, handle)
func (r *Router) Put(path string, handle http.Handler) {
	r.Handle(http.MethodPut, path, handle)
}

// Patch is a shortcut for router.Handle(http.MethodPatch, path, handle)
func (r *Router) Patch(path string, handle http.Handler) {
	r.Handle(http.MethodPatch, path, handle)
}

// Delete is a shortcut for router.Handle(http.MethodDelete, path, handle)
func (r *Router) Delete(path string, handle http.Handler) {
	r.Handle(http.MethodDelete, path, handle)
}

// GetAndHead is a shortcut for router.Get(path, handle) and router.Head(path, handle)
func (r *Router) GetAndHead(path string, handle http.Handler) {
	r.Handle(http.MethodGet, path, handle)
	r.Handle(http.MethodHead, path, handle)
}

// Handle registers a new request handle with the given path and method.
//
// For GET, POST, PUT, PATCH and DELETE requests the respective shortcut
// functions can be used.
//
// This function is intended for bulk loading and to allow the usage of less
// frequently used, non-standardized or custom methods (e.g. for internal
// communication with a proxy).
func (r *Router) Handle(method, path string, handle http.Handler) {
	if path[0] != '/' {
		panic("path must begin with '/' in path '" + path + "'")
	}

	if r.trees == nil {
		r.trees = make(map[string]*node)
	}

	root := r.trees[method]
	if root == nil {
		root = new(node)
		r.trees[method] = root
	}

	root.addRoute(path, handle)
}

// HandlerFunc is an adapter which allows the usage of an http.HandlerFunc as a
// request handle.
func (r *Router) HandlerFunc(method, path string, handler http.HandlerFunc) {
	r.Handle(method, path, handler)
}

// ServeFiles serves files from the given file system root.
// The path must end with "/*filepath", files are then served from the local
// path /defined/root/dir/*filepath.
// For example if root is "/etc" and *filepath is "passwd", the local file
// "/etc/passwd" would be served.
// Internally a http.FileServer is used, therefore http.NotFound is used instead
// of the Router's NotFound handler.
// To use the operating system's file system implementation,
// use http.Dir:
//     router.ServeFiles("/src/*filepath", http.Dir("/var/www"))
func (r *Router) ServeFiles(path string, root http.FileSystem) {
	if len(path) < 10 || path[len(path)-10:] != "/*filepath" {
		panic("path must end with /*filepath in path '" + path + "'")
	}

	r.GetAndHead(path, PathHandler(http.FileServer(root)))
}

func (r *Router) recv(w http.ResponseWriter, req *http.Request) {
	if rcv := recover(); rcv != nil {
		ctx := context.WithValue(req.Context(), panicKey, rcv)
		r.PanicHandler.ServeHTTP(w, req.WithContext(ctx))
	}
}

// Lookup allows the manual lookup of a method + path combo.
// This is e.g. useful to build a framework around this router.
// If the path was found, it returns the handle function and the path parameter
// values. Otherwise the third return value indicates whether a redirection to
// the same path with an extra / without the trailing slash should be performed.
func (r *Router) Lookup(method, path string) (http.Handler, Params, bool) {
	if root := r.trees[method]; root != nil {
		return root.getValue(path)
	}
	return nil, nil, false
}

func (r *Router) allowed(path, reqMethod string) (allow string) {
	if path == "*" { // server-wide
		for method := range r.trees {
			if method == http.MethodOptions {
				continue
			}

			// add request method to list of allowed methods
			if len(allow) == 0 {
				allow = method
			} else {
				allow += ", " + method
			}
		}
	} else { // specific path
		for method := range r.trees {
			// Skip the requested method - we already tried this one
			if method == reqMethod || method == http.MethodOptions {
				continue
			}

			handle, _, _ := r.trees[method].getValue(path)
			if handle != nil {
				// add request method to list of allowed methods
				if len(allow) == 0 {
					allow = method
				} else {
					allow += ", " + method
				}
			}
		}
	}
	if len(allow) > 0 {
		allow += ", OPTIONS"
	}
	return
}

// ServeHTTP makes the router implement the http.Handler interface.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.PanicHandler != nil {
		defer r.recv(w, req)
	}

	path := req.URL.Path

	if root := r.trees[req.Method]; root != nil {
		if handler, ps, tsr := root.getValue(path); handler != nil {
			if ps != nil {
				req = req.WithContext(&paramsContext{req.Context(), ps})
			}

			handler.ServeHTTP(w, req)
			return
		} else if req.Method != http.MethodConnect && path != "/" {
			code := http.StatusMovedPermanently // Permanent redirect, request with GET method
			if req.Method != http.MethodGet {
				// Temporary redirect, request with same method
				// As of Go 1.3, Go does not support status code 308.
				code = http.StatusTemporaryRedirect
			}

			if tsr && r.RedirectTrailingSlash {
				u := *req.URL

				if len(path) > 1 && path[len(path)-1] == '/' {
					u.Path = path[:len(path)-1]
				} else {
					u.Path = path + "/"
				}

				http.Redirect(w, req, u.String(), code)
				return
			}

			// Try to fix the request path
			if r.RedirectFixedPath {
				fixedPath, found := root.findCaseInsensitivePath(
					CleanPath(path),
					r.RedirectTrailingSlash,
				)
				if found {
					u := *req.URL
					u.Path = string(fixedPath)

					http.Redirect(w, req, u.String(), code)
					return
				}
			}
		}
	}

	if req.Method == http.MethodOptions {
		// Handle OPTIONS requests
		if r.HandleOptions {
			if allow := r.allowed(path, req.Method); len(allow) > 0 {
				w.Header().Set("Allow", allow)
				return
			}
		}
	} else {
		// Handle 405
		if r.HandleMethodNotAllowed {
			if allow := r.allowed(path, req.Method); len(allow) > 0 {
				w.Header().Set("Allow", allow)
				if r.MethodNotAllowed != nil {
					r.MethodNotAllowed.ServeHTTP(w, req)
				} else {
					http.Error(w,
						http.StatusText(http.StatusMethodNotAllowed),
						http.StatusMethodNotAllowed,
					)
				}
				return
			}
		}
	}

	// Handle 404
	if r.NotFound != nil {
		r.NotFound.ServeHTTP(w, req)
	} else {
		http.NotFound(w, req)
	}
}
