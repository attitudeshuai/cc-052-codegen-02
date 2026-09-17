package router

import (
	"cc-052/internal/handler"
	"cc-052/internal/middleware"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

func Setup(
	farmH *handler.FarmHandler,
	plotH *handler.PlotHandler,
	batchH *handler.BatchHandler,
	activityH *handler.ActivityHandler,
	inspectionH *handler.InspectionHandler,
	traceCodeH *handler.TraceCodeHandler,
	machineH *handler.MachineHandler,
	reservationH *handler.ReservationHandler,
	healthH *handler.HealthHandler,
	rdb *redis.Client,
) *gin.Engine {
	r := gin.Default()

	// Health check
	r.GET("/healthz", healthH.Health)

	// API v1
	v1 := r.Group("/api/v1")
	{
		// Farms
		v1.POST("/farms", farmH.Create)
		v1.GET("/farms", farmH.List)
		v1.GET("/farms/:id", farmH.GetByID)

		// Plots
		v1.POST("/plots", plotH.Create)
		v1.GET("/plots/:id", plotH.GetByID)
		v1.GET("/plots", plotH.ListByFarm)

		// Batches
		v1.POST("/batches", batchH.Create)
		v1.GET("/batches/:id", batchH.GetByID)

		// Activities
		v1.POST("/batches/:id/activities", activityH.Create)
		v1.POST("/batches/:id/activities/batch", activityH.BatchCreate)
		v1.GET("/batches/:id/activities", activityH.ListByBatch)

		// Inspections
		v1.POST("/batches/:id/inspection", inspectionH.Create)

		// Trace codes
		v1.POST("/batches/:id/codes", traceCodeH.Generate)

		// Machines (农机)
		v1.POST("/machines", machineH.Create)
		v1.GET("/machines", machineH.List)
		v1.GET("/machines/:id", machineH.GetByID)
		v1.POST("/machines/:id/breakdown", machineH.Breakdown)
		v1.POST("/machines/:id/repair", machineH.Repair)
		v1.GET("/machines/:id/utilization", machineH.Utilization)
		v1.GET("/farms/:id/machine-utilization", machineH.FarmUtilization)

		// Reservations (按时段预约)
		v1.POST("/reservations", reservationH.Create)
		v1.GET("/reservations", reservationH.List)
		v1.GET("/reservations/:id", reservationH.GetByID)
		v1.POST("/reservations/:id/start", reservationH.Start)
		v1.POST("/reservations/:id/complete", reservationH.Complete)
		v1.POST("/reservations/:id/cancel", reservationH.Cancel)

		// Plot work progress (地块作业进度)
		v1.GET("/plots/:id/work-progress", reservationH.PlotProgress)
	}

	// Public trace endpoints with rate limiting
	traceGroup := r.Group("/api/v1/trace")
	traceGroup.Use(middleware.RateLimit(rdb, 30, time.Minute))
	{
		traceGroup.GET("/:code", traceCodeH.Trace)
		traceGroup.GET("/:code/validate", traceCodeH.Validate)
	}

	return r
}