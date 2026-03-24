package admin

import (
	"encoding/json"
	"net/http"
)

type integratedErrorCode string

const (
	errCodeInvalidToken       integratedErrorCode = "INVALID_TOKEN"
	errCodeMissingHeader      integratedErrorCode = "MISSING_HEADER"
	errCodeInvalidGatewayRole integratedErrorCode = "INVALID_GATEWAY_ROLE"
	errCodeInsufficientRole   integratedErrorCode = "INSUFFICIENT_ROLE"
	errCodeInvalidRequest     integratedErrorCode = "INVALID_REQUEST"
	errCodeNotFound           integratedErrorCode = "NOT_FOUND"
	errCodeModeStandalone     integratedErrorCode = "MODE_STANDALONE"
	errCodeInternalError      integratedErrorCode = "INTERNAL_ERROR"
	errCodeMethodNotAllowed   integratedErrorCode = "METHOD_NOT_ALLOWED"
)

type integratedErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeIntegratedError(w http.ResponseWriter, status int, code integratedErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(integratedErrorResponse{
		Error: message,
		Code:  string(code),
	})
}

func writeIntegratedJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
