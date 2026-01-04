package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go-nexus/internal/delivery/http"
	"go-nexus/internal/infrastructure/llm"
	"go-nexus/internal/usecase"
	"log"
)

func main() {
	viper.SetConfigFile("configs/config.yaml")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config file: %s", err)
	}

	llmConfig := &llm.Config{
		APIKey:         viper.GetString("llm.api_key"),
		BaseURL:        viper.GetString("llm.base_url"),
		Model:          viper.GetString("llm.model_name"),
		EmbeddingModel: viper.GetString("llm.embedding_model"),
	}
	llmClient := llm.NewOpenAIAdapter(llmConfig)

	ragService := usecase.NewRAGUseCase(nil, nil, llmClient)

	chatHandler := http.NewChatHandler(ragService)

	r := gin.Default()
	api := r.Group("/api/v1")
	{
		api.POST("/chat", chatHandler.Chat)
	}
	port := viper.GetString("server.port")
	fmt.Printf("Server running on port %s\n", port)
	if err := r.Run(port); err != nil {
		log.Fatal(err)
	}
}
