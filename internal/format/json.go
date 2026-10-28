package format

import (
	"bytes"
	"encoding/json"

	"github.com/bkuberek/spmeta/internal/model"
)

// JSON renders np on one line followed by a newline. A nil np (nothing
// playing) renders as "null\n". Characters such as & and < stay literal.
func JSON(np *model.NowPlaying) ([]byte, error) {
	// plain drops model.NowPlaying's MarshalJSON, which always escapes HTML.
	type plain model.NowPlaying
	var v *plain
	if np != nil {
		c := plain(*np)
		if c.Artists == nil {
			c.Artists = []string{}
		}
		v = &c
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
