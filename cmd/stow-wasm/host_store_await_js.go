package main

import (
	"context"
	"fmt"
	"syscall/js"
)

type promiseResult struct {
	value js.Value
	err   error
}

func awaitHostPromise(ctx context.Context, promise js.Value) (js.Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	settled := make(chan promiseResult, 1)
	resolved := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		value := js.Undefined()
		if len(args) > 0 {
			value = args[0]
		}
		settled <- promiseResult{value: value}
		return nil
	})
	rejected := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		message := "host promise rejected"
		if len(args) > 0 {
			value := args[0]
			if value.Type() == js.TypeObject && value.Get("message").Type() == js.TypeString {
				message = value.Get("message").String()
			} else {
				message = value.String()
			}
		}
		settled <- promiseResult{err: fmt.Errorf("%s", message)}
		return nil
	})
	js.Global().Get("Promise").Call("resolve", promise).Call("then", resolved, rejected)
	select {
	case result := <-settled:
		resolved.Release()
		rejected.Release()
		return result.value, result.err
	case <-ctx.Done():
		go func() { <-settled; resolved.Release(); rejected.Release() }()
		return js.Undefined(), ctx.Err()
	}
}
