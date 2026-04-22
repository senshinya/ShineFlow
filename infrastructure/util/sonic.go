package util

import (
	"context"

	"github.com/bytedance/sonic"
)

var json = sonic.Config{
	UseInt64: true,
}.Froze()

func MarshalToString(_ context.Context, source any) (string, error) {
	return json.MarshalToString(source)
}

func UnmarshalFromString(_ context.Context, str string, target any) error {
	return json.UnmarshalFromString(str, target)
}
