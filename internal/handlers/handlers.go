package handlers

import (
	"net/http"
	"review-service/internal/models"
	"review-service/internal/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *service.Service
}

func NewHandler(service *service.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateTeam(c *gin.Context) {
	var req models.CreateTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, createError("INVALID_INPUT", err.Error()))
		return
	}

	team, err := h.service.CreateTeam(c.Request.Context(), req)
	if err != nil {
		switch err {
		case service.ErrTeamExists:
			c.JSON(http.StatusBadRequest, createError("TEAM_EXISTS", "team_name already exists"))
		default:
			c.JSON(http.StatusInternalServerError, createError("INTERNAL_ERROR", err.Error()))
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"team": team})
}

func (h *Handler) GetTeam(c *gin.Context) {
	teamName := c.Query("team_name")
	if teamName == "" {
		c.JSON(http.StatusBadRequest, createError("MISSING_PARAM", "team_name is required"))
		return
	}

	team, err := h.service.GetTeam(c.Request.Context(), teamName)
	if err != nil {
		c.JSON(http.StatusNotFound, createError("NOT_FOUND", "team not found"))
		return
	}

	c.JSON(http.StatusOK, team)
}

func (h *Handler) SetUserActive(c *gin.Context) {
	var req models.SetUserActiveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, createError("INVALID_INPUT", err.Error()))
		return
	}

	user, err := h.service.SetUserActive(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusNotFound, createError("NOT_FOUND", "user not found"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) CreatePR(c *gin.Context) {
	var req models.CreatePRRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, createError("INVALID_INPUT", err.Error()))
		return
	}

	pr, err := h.service.CreatePR(c.Request.Context(), req)
	if err != nil {
		switch err {
		case service.ErrPRExists:
			c.JSON(http.StatusConflict, createError("PR_EXISTS", "PR id already exists"))
		case service.ErrUserNotFound:
			c.JSON(http.StatusNotFound, createError("NOT_FOUND", "author/team not found"))
		default:
			c.JSON(http.StatusInternalServerError, createError("INTERNAL_ERROR", err.Error()))
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"pr": pr})
}

func (h *Handler) MergePR(c *gin.Context) {
	var req models.MergePRRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, createError("INVALID_INPUT", err.Error()))
		return
	}

	pr, err := h.service.MergePR(c.Request.Context(), req.PullRequestID)
	if err != nil {
		c.JSON(http.StatusNotFound, createError("NOT_FOUND", "PR not found"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"pr": pr})
}

func (h *Handler) ReassignReviewer(c *gin.Context) {
	var req models.ReassignReviewerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, createError("INVALID_INPUT", err.Error()))
		return
	}

	pr, newReviewerID, err := h.service.ReassignReviewer(c.Request.Context(), req)
	if err != nil {
		switch err {
		case service.ErrPRNotFound:
			c.JSON(http.StatusNotFound, createError("NOT_FOUND", "PR not found"))
		case service.ErrPRMerged:
			c.JSON(http.StatusConflict, createError("PR_MERGED", "cannot reassign on merged PR"))
		case service.ErrReviewerNotAssigned:
			c.JSON(http.StatusConflict, createError("NOT_ASSIGNED", "reviewer is not assigned to this PR"))
		case service.ErrNoCandidate:
			c.JSON(http.StatusConflict, createError("NO_CANDIDATE", "no active replacement candidate in team"))
		case service.ErrUserNotFound:
			c.JSON(http.StatusNotFound, createError("NOT_FOUND", "user not found"))
		default:
			c.JSON(http.StatusInternalServerError, createError("INTERNAL_ERROR", err.Error()))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"pr":          pr,
		"replaced_by": newReviewerID,
	})
}

func (h *Handler) GetUserPRs(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, createError("MISSING_PARAM", "user_id is required"))
		return
	}

	response, err := h.service.GetUserPRs(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, createError("INTERNAL_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *Handler) HealthCheck(c *gin.Context) {
	err := h.service.CheckHealth(c.Request.Context())
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	c.JSON(http.StatusServiceUnavailable, createError("INTERNAL_ERROR", err.Error()))
}

func createError(code, message string) models.ErrorResponse {
	var errResp models.ErrorResponse
	errResp.Error.Code = code
	errResp.Error.Message = message
	return errResp
}
