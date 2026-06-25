package capture

import "errors"

// IngestPath is the canonical v1 ingest endpoint.
const IngestPath = "/api/v1/contexts/sessions"

// Poster is the minimal HTTP surface the ingest client needs. *api.Caller
// satisfies it; tests provide a fake.
type Poster interface {
	Post(path string, body interface{}, result interface{}) error
}

// Ingest POSTs a capture to the backend and returns the parsed response. It
// fills the canonical ContentHash before sending if the caller left it empty.
func Ingest(p Poster, req *IngestRequest) (*IngestResponse, error) {
	if req == nil {
		return nil, errors.New("nil ingest request")
	}
	if req.ContentHash == "" {
		req.ContentHash = req.ComputeContentHash()
	}
	var resp IngestResponse
	if err := p.Post(IngestPath, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
