package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	v1 "go-nexus/internal/delivery/http/v1"
	"go-nexus/internal/infrastructure/llm"
	"go-nexus/internal/infrastructure/persistence"
	"go-nexus/internal/usecase"
	"go-nexus/pkg/database"
	"log"
)

func main() {
	viper.SetConfigFile("configs/config.yaml")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config file: %s", err)
	}

	dsn := fmt.Sprintf("host=localhost user=nexus password=nexus_password dbname=nexus_db port=5432 sslmode=disable")
	db := database.NewPostgresDB(dsn)
	err := db.AutoMigrate(&database.DocumentModel{}, &database.DocumentChunkModel{})
	if err != nil {
		log.Fatalf("failed to migrate db: %v", err)
	}
	repo := persistence.NewPostgresRepo(db)

	llmConfig := &llm.Config{
		APIKey:         viper.GetString("llm.api_key"),
		BaseURL:        viper.GetString("llm.base_url"),
		Model:          viper.GetString("llm.model_name"),
		EmbeddingModel: viper.GetString("llm.embedding_model"),
	}
	llmClient := llm.NewOpenAIAdapter(llmConfig)

	ragService := usecase.NewRAGUseCase(repo, repo, llmClient)
	ingestService := usecase.NewIngestService(ragService, 5)

	chatHandler := v1.NewChatHandler(ragService)
	uploadHandler := v1.NewUploadHandler(ingestService)

	r := gin.Default()
	api := r.Group("/api/v1")
	{
		api.POST("/chat", chatHandler.Chat)
		api.POST("/knowledge", chatHandler.AddKnowledge)
		api.POST("/upload", uploadHandler.Upload)
	}
	port := viper.GetString("server.port")
	fmt.Printf("Server running on port %s\n", port)
	if err := r.Run(port); err != nil {
		log.Fatal(err)
	}
}
