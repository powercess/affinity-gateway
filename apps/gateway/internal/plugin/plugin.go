// Package plugin runs sandboxed Lua request transformation plugins.
//
// The Go core owns session affinity and request forwarding; plugins only decide
// how a request is shaped on its way out. Lua code never gets network, file or
// OS access: it receives a mutable request view and a couple of helpers.
package plugin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/config"
)

const runTimeout = 250 * time.Millisecond

// Context is the mutable request view exposed to Lua plugins.
type Context struct {
	Direction     string
	RouteID       string
	Session       string
	SessionSource string
	Method        string
	Path          string
	Query         map[string]string
	Headers       map[string]string
	Body          []byte
}

// SetHeader sets a header (case-insensitive key).
func (c *Context) SetHeader(name, value string) {
	c.Headers[strings.ToLower(name)] = value
}

// RemoveHeader removes a header (case-insensitive key).
func (c *Context) RemoveHeader(name string) {
	delete(c.Headers, strings.ToLower(name))
}

// Result is the outcome of a plugin chain.
type Result struct {
	Action  string
	Status  int
	Message string
}

// Program is a compiled plugin.
type Program struct {
	ID    string
	proto *lua.FunctionProto
}

// Compile parses Lua source into a reusable program.
func Compile(id, source string) (*Program, error) {
	chunk, err := parse.Parse(strings.NewReader(source), id)
	if err != nil {
		return nil, fmt.Errorf("Lua 解析失败: %w", err)
	}
	proto, err := lua.Compile(chunk, id)
	if err != nil {
		return nil, fmt.Errorf("Lua 编译失败: %w", err)
	}
	return &Program{ID: id, proto: proto}, nil
}

// ValidateSource compiles the source and runs it once against a dummy request
// so configuration errors surface at save time.
func ValidateSource(id, source string) error {
	program, err := Compile(id, source)
	if err != nil {
		return err
	}
	dummy := &Context{
		Direction: config.Inbound,
		RouteID:   "validate",
		Method:    "POST",
		Path:      "/validate",
		Query:     map[string]string{},
		Headers:   map[string]string{},
		Body:      []byte(`{}`),
	}
	if _, err := program.Run(context.Background(), dummy, "validation-secret"); err != nil {
		return err
	}
	return nil
}

// Run executes the program against rc.
func (p *Program) Run(parent context.Context, rc *Context, secret string) (Result, error) {
	ctx, cancel := context.WithTimeout(parent, runTimeout)
	defer cancel()

	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	L.SetContext(ctx)

	ctxTable := newSandbox(L, secret, rc)
	fn := L.NewFunctionFromProto(p.proto)
	L.Push(fn)
	if err := L.PCall(0, 0, nil); err != nil {
		return Result{}, err
	}
	handle := L.GetGlobal("handle")
	if handle.Type() != lua.LTFunction {
		return Result{}, errors.New("插件必须定义 handle(ctx)")
	}
	L.Push(handle)
	L.Push(ctxTable)
	if err := L.PCall(1, 1, nil); err != nil {
		return Result{}, err
	}
	ret := L.Get(-1)
	L.Pop(1)

	result := Result{Action: "continue"}
	if s, ok := ret.(lua.LString); ok {
		switch strings.ToLower(string(s)) {
		case "continue":
			result.Action = "continue"
		case "reject", "stop":
			result.Action = "reject"
		default:
			result.Action = "continue"
		}
	}
	return result, nil
}

// newSandbox opens a limited standard library and builds the ctx table.
func newSandbox(L *lua.LState, secret string, rc *Context) *lua.LTable {
	lua.OpenBase(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	lua.OpenMath(L)
	for _, name := range []string{"dofile", "loadfile", "require", "collectgarbage", "print"} {
		L.SetGlobal(name, lua.LNil)
	}

	jsonTable := L.NewTable()
	L.SetField(jsonTable, "decode", L.NewFunction(luaJSONDecode))
	L.SetField(jsonTable, "encode", L.NewFunction(luaJSONEncode))
	L.SetGlobal("json", jsonTable)

	sessionTable := L.NewTable()
	L.SetField(sessionTable, "opencode", L.NewFunction(func(L *lua.LState) int {
		value := L.CheckString(1)
		binding := L.OptString(2, "")
		L.Push(lua.LString(OpenCodeSessionID(secret, binding, value)))
		return 1
	}))
	L.SetGlobal("session", sessionTable)

	ctxTable := L.NewTable()
	L.SetField(ctxTable, "direction", lua.LString(rc.Direction))
	L.SetField(ctxTable, "route_id", lua.LString(rc.RouteID))
	L.SetField(ctxTable, "session", lua.LString(rc.Session))
	L.SetField(ctxTable, "session_source", lua.LString(rc.SessionSource))
	L.SetField(ctxTable, "method", lua.LString(rc.Method))
	L.SetField(ctxTable, "path", lua.LString(rc.Path))
	L.SetField(ctxTable, "body", lua.LString(string(rc.Body)))
	L.SetField(ctxTable, "headers", stringMapToTable(L, rc.Headers))
	L.SetField(ctxTable, "query", stringMapToTable(L, rc.Query))
	L.SetField(ctxTable, "set_header", L.NewFunction(func(L *lua.LState) int {
		rc.SetHeader(L.CheckString(2), L.CheckString(3))
		return 0
	}))
	L.SetField(ctxTable, "remove_header", L.NewFunction(func(L *lua.LState) int {
		rc.RemoveHeader(L.CheckString(2))
		return 0
	}))
	L.SetField(ctxTable, "set_body", L.NewFunction(func(L *lua.LState) int {
		rc.Body = []byte(L.CheckString(2))
		return 0
	}))
	return ctxTable
}

// Registry holds the compiled plugins and runs route chains.
type Registry struct {
	mu       sync.RWMutex
	programs map[string]*Program
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{programs: map[string]*Program{}}
}

// Load compiles every definition. Definitions that fail to compile are skipped
// and reported in the returned error so the rest stay usable.
func (r *Registry) Load(defs []config.Plugin) error {
	compiled := make(map[string]*Program, len(defs))
	var failures []string
	for _, def := range defs {
		program, err := Compile(def.ID, def.Source)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", def.ID, err))
			continue
		}
		compiled[def.ID] = program
	}
	r.mu.Lock()
	r.programs = compiled
	r.mu.Unlock()
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

// RunChain runs the given plugin ids in order over rc.
func (r *Registry) RunChain(ctx context.Context, ids []string, rc *Context, secret string) (Result, error) {
	result := Result{Action: "continue"}
	for _, id := range ids {
		r.mu.RLock()
		program := r.programs[id]
		r.mu.RUnlock()
		if program == nil {
			continue
		}
		step, err := program.Run(ctx, rc, secret)
		if err != nil {
			return Result{}, fmt.Errorf("插件 %s: %w", id, err)
		}
		if step.Action == "reject" {
			return step, nil
		}
	}
	return result, nil
}

const opencodeIDBase62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// OpenCodeSessionID derives a stable, OpenCode-shaped session id from the
// gateway secret and an affinity identity. It never exposes the source id.
func OpenCodeSessionID(secret, binding, affinity string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	for _, part := range []string{"opencode-session:v1", binding, affinity} {
		writeLengthPrefixed(mac, part)
	}
	sum := mac.Sum(nil)

	const hexChars = "0123456789abcdef"
	hexPart := make([]byte, 12)
	for i := 0; i < 6; i++ {
		hexPart[i*2] = hexChars[sum[i]>>4]
		hexPart[i*2+1] = hexChars[sum[i]&15]
	}
	base62Part := make([]byte, 14)
	for i := range base62Part {
		base62Part[i] = opencodeIDBase62[int(sum[6+i])%len(opencodeIDBase62)]
	}
	return "ses_" + string(hexPart) + string(base62Part)
}

func writeLengthPrefixed(mac interface{ Write([]byte) (int, error) }, part string) {
	length := len(part)
	_, _ = mac.Write([]byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)})
	_, _ = mac.Write([]byte(part))
}

func stringMapToTable(L *lua.LState, values map[string]string) *lua.LTable {
	table := L.NewTable()
	for key, value := range values {
		L.SetField(table, key, lua.LString(value))
	}
	return table
}

func luaJSONDecode(L *lua.LState) int {
	source := L.CheckString(1)
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(toLua(L, value))
	return 1
}

func luaJSONEncode(L *lua.LState) int {
	value := fromLua(L.CheckAny(1))
	raw, err := json.Marshal(value)
	if err != nil {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LString(string(raw)))
	return 1
}

func toLua(L *lua.LState, value any) lua.LValue {
	switch typed := value.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(typed)
	case string:
		return lua.LString(typed)
	case float64:
		return lua.LNumber(typed)
	case json.Number:
		f, err := typed.Float64()
		if err != nil {
			return lua.LString(typed.String())
		}
		return lua.LNumber(f)
	case map[string]any:
		table := L.NewTable()
		for key, item := range typed {
			L.SetField(table, key, toLua(L, item))
		}
		return table
	case []any:
		table := L.NewTable()
		for _, item := range typed {
			table.Append(toLua(L, item))
		}
		return table
	default:
		return lua.LNil
	}
}

func fromLua(value lua.LValue) any {
	switch typed := value.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(typed)
	case lua.LNumber:
		return float64(typed)
	case lua.LString:
		return string(typed)
	case *lua.LTable:
		if typed.Len() > 0 {
			items := make([]any, 0, typed.Len())
			for i := 1; i <= typed.Len(); i++ {
				items = append(items, fromLua(typed.RawGetInt(i)))
			}
			return items
		}
		object := map[string]any{}
		typed.ForEach(func(key, item lua.LValue) {
			object[key.String()] = fromLua(item)
		})
		return object
	default:
		return nil
	}
}
