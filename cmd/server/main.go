package main

import (
	"context"
	"fmt"
	"go-nexus/internal/delivery/http"
	v1 "go-nexus/internal/delivery/http/v1"
	"go-nexus/internal/domain"
	"go-nexus/internal/infrastructure/graph"
	"go-nexus/internal/infrastructure/llm"
	"go-nexus/internal/infrastructure/persistence"
	"go-nexus/internal/usecase"
	"go-nexus/internal/usecase/tools"
	"go-nexus/internal/workflow"
	"go-nexus/internal/workflow/activities"
	"go-nexus/pkg/database"
	"go-nexus/pkg/telemetry"
	"log"
	"os"

	"github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func main() {
	// 初始化 Jaeger 追踪
	jaegerEndpoint := os.Getenv("JAEGER_ENDPOINT")
	if jaegerEndpoint == "" {
		jaegerEndpoint = "localhost:4318"
	}
	shutdown := telemetry.InitTracer("go-nexus-service", jaegerEndpoint)
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer: %v", err)
		}
	}()

	// 加载配置文件（支持通过环境变量指定配置文件路径）
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Warning: Error reading config file: %s, using environment variables only", err)
	}

	// 支持环境变量覆盖配置
	viper.AutomaticEnv()
	viper.SetEnvPrefix("")

	// 配置数据库连接（优先使用环境变量，适合 Docker 环境）
	dbHost := getEnvOrDefault("DB_HOST", "localhost")
	dbUser := getEnvOrDefault("DB_USER", "nexus")
	dbPassword := getEnvOrDefault("DB_PASSWORD", "nexus_password")
	dbName := getEnvOrDefault("DB_NAME", "nexus_db")
	dbPort := getEnvOrDefault("DB_PORT", "5432")
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		dbHost, dbUser, dbPassword, dbName, dbPort)

	db := database.NewPostgresDB(dsn)
	err := db.AutoMigrate(&database.DocumentModel{}, &database.DocumentChunkModel{}, &domain.MorningBrief{})
	if err != nil {
		log.Fatalf("failed to migrate db: %v", err)
	}
	repo := persistence.NewPostgresRepo(db)
	briefRepo := persistence.NewPostgresBriefRepo(db)
	// 配置 LLM（优先使用环境变量，适合 Docker 环境）
	llmConfig := &llm.Config{
		APIKey:         getEnvOrDefault("LLM_API_KEY", viper.GetString("llm.api_key")),
		BaseURL:        getEnvOrDefault("LLM_BASE_URL", viper.GetString("llm.base_url")),
		Model:          getEnvOrDefault("LLM_MODEL", viper.GetString("llm.model_name")),
		EmbeddingModel: getEnvOrDefault("LLM_EMBEDDING_MODEL", viper.GetString("llm.embedding_model")),
	}
	llmClient := llm.NewOpenAIAdapter(llmConfig)

	// 初始化 Neo4j 图数据库（可选，如果连接失败则设为 nil）
	neo4jURI := getEnvOrDefault("NEO4J_URI", "bolt://localhost:7687")
	neo4jUser := getEnvOrDefault("NEO4J_USER", "neo4j")
	neo4jPassword := getEnvOrDefault("NEO4J_PASSWORD", "nexus_password")
	graphRepo, err := graph.NewNeo4jRepo(neo4jURI, neo4jUser, neo4jPassword)
	if err != nil {
		log.Fatalf("Failed to connect to Neo4j: %v", err)
	}
	defer graphRepo.Close(context.Background())

	jinaKey := os.Getenv("JINA_API_KEY")
	reranker := llm.NewJinaReranker(jinaKey)
	ragService := usecase.NewRAGUseCase(repo, repo, llmClient, graphRepo, reranker)
	ingestService := usecase.NewIngestService(ragService, 5)
	agentService := usecase.NewAgentUseCase(llmClient, ragService)
	chatHandler := v1.NewChatHandler(ragService)
	uploadHandler := v1.NewUploadHandler(ingestService)

	temporalHost := os.Getenv("TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}

	tClient, err := client.Dial(client.Options{
		HostPort: temporalHost,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer tClient.Close()
	w := worker.New(tClient, "agent-task-queue", worker.Options{})

	redisAddr := getEnvOrDefault("REDIS_HOST", "localhost:6379")
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	agentActivities := activities.NewAgentActivities(agentService, rdb)
	dataActivities := activities.NewDataActivities(agentService, briefRepo, rdb)
	wsHandler := http.NewWSHandler(rdb, tClient)

	// 初始化 Python 工具（必须在 worker 启动前完成）
	if err := tools.InitPythonTool(); err != nil {
		log.Fatalf("Failed to init python tool (is Docker running?): %v", err)
	}

	w.RegisterWorkflow(workflow.MultiAgentWorkflow)
	w.RegisterActivity(agentActivities.SupervisorDecide)
	w.RegisterActivity(agentActivities.ResearcherSearch)
	w.RegisterActivity(agentActivities.CoderRun)
	w.RegisterWorkflow(workflow.StreamMultiAgentWorkflow)
	w.RegisterActivity(agentActivities.SupervisorDecideStream)
	w.RegisterActivity(agentActivities.WorkerRunStream)
	w.RegisterActivity(agentActivities.FinalReplyStream)
	w.RegisterActivity(dataActivities.FetchAndIngest)
	w.RegisterWorkflow(workflow.ScheduledDataIngestion)
	go func() {
		if err := w.Run(worker.InterruptCh()); err != nil {
			log.Fatalf("Unable to start worker: %v", err)
		}
	}()
	agentHandler := v1.NewAgentHandler(agentService, tClient)
	r := gin.Default()
	api := r.Group("/api/v1")
	{
		api.POST("/chat", chatHandler.Chat)
		api.POST("/knowledge", chatHandler.AddKnowledge)
		api.POST("/upload", uploadHandler.Upload)
		api.POST("/agent", agentHandler.Chat)
		api.POST("/multi-agent", agentHandler.MultiChat)
		api.POST("/async-chat", agentHandler.AsyncChat)
		api.POST("/agent-approve", agentHandler.Approve)
		api.GET("/ws/chat", wsHandler.HandleWS)
	}
	r.Static("/images", "./public/images")

	// 服务器端口（优先使用环境变量）
	port := getEnvOrDefault("SERVER_PORT", viper.GetString("server.port"))
	if port == "" {
		port = ":8080"
	}
	if port[0] != ':' {
		port = ":" + port
	}
	fmt.Printf("Server running on port %s\n", port)
	if err := r.Run(port); err != nil {
		log.Fatal(err)
	}
}

// getEnvOrDefault 获取环境变量，如果不存在则返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
