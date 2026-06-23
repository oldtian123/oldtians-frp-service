// Package httpx holds shared HTTP plumbing for the control-api: a consistent
// JSON error envelope, success helpers, auth middleware and token utilities.
//
// Error responses always look like:
//
//	{ "error": { "code": "bad_request", "message": "..." } }
package httpx

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/oldtian123/oldtians-frp-service/server/internal/database"
)

// Stable machine-readable error codes (documented in docs/API.md).
const (
	CodeBadRequest   = "bad_request"
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeInternal     = "internal_error"
)

// contextKey under which the authenticated gateway is stored.
const gatewayContextKey = "ofs_gateway"

// errorBody is the JSON error envelope.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error writes a JSON error envelope and aborts the request.
func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// BadRequest / Unauthorized / etc. are convenience wrappers.
func BadRequest(c *gin.Context, message string)   { Error(c, http.StatusBadRequest, CodeBadRequest, message) }
func Unauthorized(c *gin.Context, message string)  { Error(c, http.StatusUnauthorized, CodeUnauthorized, message) }
func Forbidden(c *gin.Context, message string)     { Error(c, http.StatusForbidden, CodeForbidden, message) }
func NotFound(c *gin.Context, message string)      { Error(c, http.StatusNotFound, CodeNotFound, message) }
func Conflict(c *gin.Context, message string)      { Error(c, http.StatusConflict, CodeConflict, message) }
func Internal(c *gin.Context, message string)      { Error(c, http.StatusInternalServerError, CodeInternal, message) }

// OK writes a 200 response with the given body.
func OK(c *gin.Context, body any) { c.JSON(http.StatusOK, body) }

// Created writes a 201 response with the given body.
func Created(c *gin.Context, body any) { c.JSON(http.StatusCreated, body) }

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(h) >= len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return strings.TrimSpace(h)
}

// ConstantTimeEqual compares two strings without leaking length-independent timing.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// AdminAuth returns middleware enforcing the admin bearer token.
func AdminAuth(adminToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tok := bearerToken(c)
		if tok == "" || !ConstantTimeEqual(tok, adminToken) {
			Unauthorized(c, "invalid or missing admin token")
			return
		}
		c.Next()
	}
}

// GatewayAuth returns middleware that authenticates a gateway by its :gateway_id
// path parameter and bearer secret. On success the gateway is stored in context.
func GatewayAuth(store *database.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("gateway_id")
		if id == "" {
			BadRequest(c, "missing gateway_id")
			return
		}
		tok := bearerToken(c)
		if tok == "" {
			Unauthorized(c, "missing gateway secret")
			return
		}
		g, err := store.GetGateway(id)
		if errors.Is(err, database.ErrNotFound) {
			Unauthorized(c, "unknown gateway or invalid secret")
			return
		}
		if err != nil {
			Internal(c, "failed to load gateway")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(g.SecretHash), []byte(tok)) != nil {
			Unauthorized(c, "unknown gateway or invalid secret")
			return
		}
		c.Set(gatewayContextKey, g)
		c.Next()
	}
}

// GatewayFromContext returns the gateway placed in context by GatewayAuth.
func GatewayFromContext(c *gin.Context) (*database.Gateway, bool) {
	v, ok := c.Get(gatewayContextKey)
	if !ok {
		return nil, false
	}
	g, ok := v.(*database.Gateway)
	return g, ok
}

// HashSecret returns the bcrypt hash of a gateway secret.
func HashSecret(secret string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	return string(b), err
}
