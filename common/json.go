package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/gin-gonic/gin/binding"
)

type RawMessage = json.RawMessage

// hostJSONCodec is the single place where the host chooses its JSON engine.
// Swap the implementation here (for example to sonic.ConfigStd) and every
// common.* and kitutil.* JSON helper, including relaykit DTO (un)marshalling,
// follows. Injected from init() rather than main() so tests run on the same
// engine as production: common is imported by virtually every root package
// and test binary, while main() never executes under `go test`.
type hostJSONCodec struct{}

func (hostJSONCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (hostJSONCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (hostJSONCodec) Decode(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

func (hostJSONCodec) Valid(data []byte) bool {
	return json.Valid(data)
}

func init() {
	kitutil.SetCodec(hostJSONCodec{})
}

func Unmarshal(data []byte, v any) error {
	return kitutil.Unmarshal(data, v)
}

func UnmarshalJsonStr(data string, v any) error {
	return kitutil.UnmarshalJsonStr(data, v)
}

func DecodeJson(reader io.Reader, v any) error {
	return kitutil.DecodeJson(reader, v)
}

// DecodeJsonWithValidation decodes JSON and applies Gin's configured binding-tag
// validator, including binding:"required" and any registered custom validators.
func DecodeJsonWithValidation(reader io.Reader, v any) error {
	if err := DecodeJson(reader, v); err != nil {
		return err
	}
	if binding.Validator == nil {
		return nil
	}
	return binding.Validator.ValidateStruct(v)
}

func Marshal(v any) ([]byte, error) {
	return kitutil.Marshal(v)
}

func IndentJson(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, data, "", "  "); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func GetJsonType(data RawMessage) string {
	return kitutil.GetJsonType(data)
}

// ValidateJSONNoDuplicateKeys rejects ambiguous JSON objects before they are
// decoded into maps or structs, where later duplicate values would otherwise
// silently replace earlier values.
func ValidateJSONNoDuplicateKeys(data []byte) error {
	type container struct {
		object       bool
		expectingKey bool
		keys         map[string]struct{}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	stack := make([]container, 0, 8)
	rootValues := 0
	completeValue := func() error {
		if len(stack) == 0 {
			rootValues++
			if rootValues > 1 {
				return errors.New("JSON contains more than one root value")
			}
			return nil
		}
		index := len(stack) - 1
		if stack[index].object {
			if stack[index].expectingKey {
				return errors.New("JSON object value is missing")
			}
			stack[index].expectingKey = true
		}
		return nil
	}

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				if err := completeValue(); err != nil {
					return err
				}
				stack = append(stack, container{object: true, expectingKey: true, keys: make(map[string]struct{})})
			case '[':
				if err := completeValue(); err != nil {
					return err
				}
				stack = append(stack, container{})
			case '}', ']':
				if len(stack) == 0 {
					return errors.New("JSON container is unbalanced")
				}
				current := stack[len(stack)-1]
				if delimiter == '}' && (!current.object || !current.expectingKey) {
					return errors.New("JSON object is incomplete")
				}
				if delimiter == ']' && current.object {
					return errors.New("JSON container delimiter is invalid")
				}
				stack = stack[:len(stack)-1]
			}
			continue
		}

		if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectingKey {
			key, ok := token.(string)
			if !ok {
				return errors.New("JSON object key must be a string")
			}
			index := len(stack) - 1
			if _, exists := stack[index].keys[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			stack[index].keys[key] = struct{}{}
			stack[index].expectingKey = false
			continue
		}
		if err := completeValue(); err != nil {
			return err
		}
	}
	if len(stack) != 0 || rootValues != 1 {
		return errors.New("JSON must contain exactly one complete root value")
	}
	return nil
}

// JsonRawMessageToString returns JSON strings as their decoded value and other JSON values as raw text.
func JsonRawMessageToString(data RawMessage) string {
	return kitutil.JsonRawMessageToString(data)
}
