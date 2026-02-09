package xdk

import "encoding/json"

// Params contains operation inputs. Keys are the Python-style parameter names
// from xdk-python (for example: "id", "user_fields", "body").
type Params map[string]any

// JSON is a generic API response payload.
type JSON map[string]any

func cloneParams(in Params) Params {
	if in == nil {
		return Params{}
	}
	out := make(Params, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func toJSONMap(v any) JSON {
	switch t := v.(type) {
	case nil:
		return JSON{}
	case map[string]any:
		return JSON(t)
	case JSON:
		return t
	default:
		return JSON{"data": v}
	}
}

func deepCloneJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}
