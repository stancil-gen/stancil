package errors

import (
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	return "validation failed"
}

func MapValidationError(err error) ValidationErrors {
	var errs ValidationErrors
	if validatorErrs, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validatorErrs {
			errs = append(errs, ValidationError{
				Field:   e.Field(),
				Message: e.Tag(),
				Code:    "VALIDATION_FAILED",
			})
		}
	}
	return errs
}

type DomainError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *DomainError) Error() string {
	return e.Message
}

func NewNotFound(resource string) *DomainError {
	return &DomainError{Code: "NOT_FOUND", Message: resource + " not found", Status: 404}
}

func NewForbidden() *DomainError {
	return &DomainError{Code: "FORBIDDEN", Message: "access denied", Status: 403}
}

func NewConflict(field string) *DomainError {
	return &DomainError{Code: "CONFLICT", Message: "conflict on " + field, Status: 409}
}

func NewUnprocessable(reason string) *DomainError {
	return &DomainError{Code: "UNPROCESSABLE_ENTITY", Message: reason, Status: 422}
}

func NewUnauthorized() *DomainError {
	return &DomainError{Code: "UNAUTHORIZED", Message: "unauthorized", Status: 401}
}

func WriteError(c *gin.Context, err error) {
	switch e := err.(type) {
	case *DomainError:
		c.JSON(e.Status, e)
	case ValidationErrors:
		c.JSON(400, gin.H{"errors": e})
	default:
		c.JSON(500, gin.H{"code": "INTERNAL_ERROR", "message": err.Error()})
	}
}
