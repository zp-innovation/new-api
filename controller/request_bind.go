package controller

import (
	"errors"
	"io"

	"github.com/gin-gonic/gin"
)

// bindOptionalRequestJSON decodes an optional JSON body; an empty body is valid
// and leaves the target zero-valued.
func bindOptionalRequestJSON(c *gin.Context, target any) error {
	err := c.ShouldBindJSON(target)
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
