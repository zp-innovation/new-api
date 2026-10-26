package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelSchedule(c *gin.Context) {
	status, err := service.ChannelScheduleStatusReport()
	if err != nil {
		common.SysError(common.LogText("failed to read the channel schedule status: %s", err.Error()))
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to load settings"))
		return
	}
	common.ApiSuccess(c, gin.H{
		"options": model.CurrentChannelSchedule().Options,
		"status":  status,
	})
}

func UpdateChannelSchedule(c *gin.Context) {
	var request struct {
		Options map[string]string `json:"options"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	if err := model.UpdateChannelScheduleOptions(request.Options); err != nil {
		common.SysError(common.LogText("failed to update the channel schedule options: %s", err.Error()))
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Failed to update settings"))
		return
	}
	GetChannelSchedule(c)
}

// RunChannelScheduleNow applies the schedule immediately. It runs the same code
// path as the timer, so a manual check cannot reach a state the schedule would
// not reach on its own.
func RunChannelScheduleNow(c *gin.Context) {
	result, err := service.RunChannelSchedule(true)
	if err != nil {
		common.SysError(common.LogText("failed to run the channel schedule: %s", err.Error()))
		common.ApiError(c, err)
		return
	}
	status, err := service.ChannelScheduleStatusReport()
	if err != nil {
		common.SysError(common.LogText("failed to read the channel schedule status: %s", err.Error()))
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to load settings"))
		return
	}
	common.ApiSuccess(c, gin.H{"result": result, "status": status})
}
