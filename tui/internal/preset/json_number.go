package preset

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// DecodeJSONUseNumber decodes exactly one JSON document while retaining number
// tokens as json.Number. The second decode preserves json.Unmarshal's rejection
// of trailing non-whitespace data instead of silently accepting another value.
func DecodeJSONUseNumber(data []byte, dst interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("invalid trailing data after JSON document")
		}
		return err
	}
	return nil
}
