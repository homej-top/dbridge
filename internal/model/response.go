package model

// Response is the unified API response format
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ErrorCode definitions
const (
	CodeSuccess          = 0
	CodeParamError       = 1000
	CodeAuthFailed       = 1001
	CodePermissionDenied = 1002
	CodeResourceNotFound = 1003
	CodeDatabaseError    = 1004
	CodeServiceUnavailable = 1005
)

func SuccessResponse(data interface{}) Response {
	return Response{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	}
}

func ErrorResponse(code int, message string) Response {
	return Response{
		Code:    code,
		Message: message,
	}
}
