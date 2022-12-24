package util

import (
	"fmt"
	"github.com/pkg/errors"
)

var panicFunc = func(v interface{}) {
	panic(v)
}

func PanicFunc(f func(v interface{})) {
	panicFunc = f
}

var errorFunc = func(v interface{}) {
	println(v)
}

func ErrorFunc(f func(v interface{})) {
	errorFunc = f
}

func Must(args ...interface{}) {
	for _, arg := range args {
		if err, ok := arg.(error); ok && err != nil {
			panicFunc(errors.WithStack(err))
			return
		}
	}
}

func Optional(args ...interface{}) bool {
	for _, arg := range args {
		if err, ok := arg.(error); ok && err != nil {
			errorFunc(err)
			return true
		}
	}
	return false
}

func MustVal[T any](rule func(T) bool, args ...interface{}) T {
	var val T
	found := false
	for _, arg := range args {
		if arg == nil {
			continue
		}
		if _, ok := arg.(T); ok && rule(arg.(T)) {
			val = arg.(T)
			found = true
			continue
		}
		Must(arg)
	}
	if !found {
		panic(fmt.Errorf("no value found"))
	}
	return val
}

func MustNotNil[T any](args ...interface{}) T {
	return MustVal[T](func(arg T) bool {
		return true
	}, args...)
}

func MustError(args ...interface{}) error {
	return MustVal[error](func(arg error) bool {
		if _, ok := arg.(error); ok {
			return true
		}
		return false
	}, args...)
}

func MustString(args ...interface{}) string {
	return MustVal[string](func(str string) bool {
		return len(str) > 0
	}, args...)
}

func MustAnyString(args ...interface{}) string {
	return MustVal[string](func(str string) bool {
		return true
	}, args...)
}

func MustBool(args ...interface{}) bool {
	return MustVal[bool](func(arg bool) bool {
		return arg
	}, args...)
}

func MustTrue(args ...interface{}) bool {
	return MustVal[bool](func(arg bool) bool {
		return arg == true
	}, args...)
}

func MustByteSlice(args ...interface{}) []byte {
	return MustVal[[]byte](func(arg []byte) bool {
		return len(arg) > 0
	}, args...)
}

func MustAnyByteSlice(args ...interface{}) []byte {
	return MustVal[[]byte](func(arg []byte) bool {
		return true
	}, args...)
}

// AnyError messy ...
func AnyError(args ...interface{}) error {
	var lastError error
	for _, arg := range args {
		if arg == nil {
			continue
		}
		if err, ok := arg.(error); ok && err != nil {
			if lastError != nil {
				lastError = errors.WithMessage(err, "")
			} else {
				lastError = err
			}
		}
	}
	return lastError
}
