package util

import (
	"github.com/bytedance/sonic"
)

var json = sonic.Config{
	UseInt64: true,
}.Froze()

func MarshalToString(source any) (string, error) {
	return json.MarshalToString(source)
}

func UnmarshalFromString(str string, target any) error {
	return json.UnmarshalFromString(str, target)
}
