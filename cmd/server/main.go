package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/utauinuneruneko/campaign_workflow_engine/internal/db"
)

func main() {
	// 1. 初始化数据库
	_, err := db.InitDB()
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}

	// 2. 设置 Gin 路由
	r := gin.Default()

	// 健康检查接口
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "campaign_workflow_engine",
		})
	})

	// 3. 启动
	log.Println("Server is running on http://localhost:8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
