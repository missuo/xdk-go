package xdk

import "context"

// Pager auto-follows next_token pagination.
type Pager struct {
	client    *Client
	op        operation
	input     Params
	nextToken string
	started   bool
	done      bool
}

func (c *Client) newPager(op operation, input Params) *Pager {
	if input == nil {
		input = Params{}
	}
	if token, ok := input["pagination_token"]; ok && !isNil(token) {
		return &Pager{
			client:    c,
			op:        op,
			input:     input,
			nextToken: toQueryValue(token),
		}
	}
	return &Pager{client: c, op: op, input: input}
}

// Next returns the next page. ok=false means pagination is exhausted.
func (p *Pager) Next(ctx context.Context) (page JSON, ok bool, err error) {
	if p.done {
		return nil, false, nil
	}

	requestInput := cloneParams(p.input)
	if p.started {
		if p.nextToken == "" {
			p.done = true
			return nil, false, nil
		}
		requestInput["pagination_token"] = p.nextToken
		if p.op.PaginationParam != "" {
			requestInput[p.op.PaginationParam] = p.nextToken
		}
	}

	resp, err := p.client.call(ctx, p.op, requestInput)
	if err != nil {
		return nil, false, err
	}
	p.started = true

	next := extractNextToken(resp)
	p.nextToken = next
	if next == "" {
		p.done = true
	}

	return resp, true, nil
}

func extractNextToken(resp JSON) string {
	meta, ok := resp["meta"]
	if !ok || meta == nil {
		return ""
	}
	metaMap, ok := meta.(map[string]any)
	if !ok {
		if m2, ok2 := meta.(JSON); ok2 {
			metaMap = map[string]any(m2)
		} else {
			return ""
		}
	}

	if v, ok := metaMap["next_token"]; ok && !isNil(v) {
		return toQueryValue(v)
	}
	return ""
}
