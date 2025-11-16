package main

import (
	"context"
	"log"
	"os"
	"review-service/internal/database"
	"review-service/internal/handlers"
	"review-service/internal/service"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func main() {
	connString := os.Getenv("DATABASE_URL")
	// connString = "postgres://user:password@db:5432/review_service?sslmode=disable"
	if connString == "" {
		log.Fatal("No database url set in enviroment")
	}

	db, err := database.NewDB(connString)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Инициализация схемы БД
	ctx := context.Background()
	if err := db.InitSchema(ctx); err != nil {
		log.Fatal("Failed to initialize database schema:", err)
	}

	svc := service.NewService(db)
	handler := handlers.NewHandler(svc)

	r := gin.Default()

	// Swagger UI с кастомной спецификацией
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler,
		ginSwagger.URL("/openapi.yaml")))

	// Эндпоинт для обслуживания OpenAPI спецификации
	r.GET("/openapi.yaml", func(c *gin.Context) {
		c.File("./openapi.yaml")
	})

	// Teams
	r.POST("/team/add", handler.CreateTeam)
	r.GET("/team/get", handler.GetTeam)

	// Users
	r.POST("/users/setIsActive", handler.SetUserActive)
	r.GET("/users/getReview", handler.GetUserPRs)

	// Pull Requests
	r.POST("/pullRequest/create", handler.CreatePR)
	r.POST("/pullRequest/merge", handler.MergePR)
	r.POST("/pullRequest/reassign", handler.ReassignReviewer)

	// Health
	r.GET("/health", handler.HealthCheck)

	log.Println("Server starting on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
