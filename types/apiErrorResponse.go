package types

type ResponseErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type ResponseErrorExtension struct {
	Code string `json:"code"`
}

type ResponseError struct {
	Message    string                  `json:"message"`
	Locations  []ResponseErrorLocation `json:"locations"`
	Extensions ResponseErrorExtension  `json:"extensions"`
}

type APIErrorResponse struct {
	Errors []ResponseError `json:"errors"`
}
