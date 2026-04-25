package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/stencil-run/stencil-go/errors"
)

var validate = validator.New()

func Bind[T any](c *gin.Context) (*T, error) {
	var req T
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, errors.NewUnprocessable(err.Error())
	}
	if err := validate.Struct(&req); err != nil {
		return nil, errors.MapValidationError(err)
	}
	return &req, nil
}

func WriteCreated(c *gin.Context, data any) {
	c.JSON(201, data)
}

func WriteOK(c *gin.Context, data any) {
	c.JSON(200, data)
}

func WriteNoContent(c *gin.Context) {
	c.Status(204)
}
