package providers

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrModelCatalogUnavailable marks providers that do not expose a model
// discovery endpoint. It is not a connection failure: callers should keep the
// manually configured or cached catalog and continue without surfacing an
// error to the user.
var ErrModelCatalogUnavailable = errors.New("el proveedor no expone un catálogo de modelos")

// IsModelCatalogUnavailable reports whether err means that model discovery is
// unsupported by the provider rather than temporarily failing.
func IsModelCatalogUnavailable(err error) bool {
	return errors.Is(err, ErrModelCatalogUnavailable)
}

func modelCatalogUnavailableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	default:
		return false
	}
}

func newModelCatalogUnavailableError(endpoint string, statusCode int) error {
	return fmt.Errorf("%w: %s respondió HTTP %d", ErrModelCatalogUnavailable, endpoint, statusCode)
}
