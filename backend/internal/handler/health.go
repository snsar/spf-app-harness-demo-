package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// healthResponse is the response body for the liveness probe.
// JSON is snake_case by convention (a single field here).
type healthResponse struct {
	Status string `json:"status"`
}

// RegisterHealthRoutes wires the health endpoints onto the given router.
// Keep handlers thin: this one does no business logic and does not touch the DB,
// so the liveness probe stays green even when MySQL is unavailable.
func RegisterHealthRoutes(r gin.IRouter) {
	r.GET("/healthz", healthz)
}

// healthz reports that the process is up. It intentionally has no dependencies.
func healthz(c *gin.Context) {
	c.JSON(http.StatusOK, healthResponse{Status: "ok"})
}
