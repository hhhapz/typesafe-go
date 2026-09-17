package typesafe

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAPIErrorOmitsProviderMessage(t *testing.T) {
	body := []byte(`{"message":"private-document-marker"}`)
	apiErr := &APIError{
		StatusCode: 429, Method: "POST", URL: "https://example.test/v1/systemone",
		RequestID: "request-123", Message: errorMessage(body), Body: body,
	}
	err := fmt.Errorf("classification failed: %w", apiErr)
	if strings.Contains(err.Error(), "private-document-marker") {
		t.Fatalf("error text exposes provider message: %s", err)
	}
	for _, want := range []string{"POST", apiErr.URL, "429", "request-123"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text is missing %q", want)
		}
	}
	var detail *APIError
	if !errors.As(err, &detail) || detail.Message != "private-document-marker" || string(detail.Body) != string(body) {
		t.Fatal("provider details are unavailable through errors.As")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatal("error lost its rate limit category")
	}
}
