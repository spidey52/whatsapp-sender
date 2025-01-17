package main

import (
	"log"
	"net/http"
	"strings"
	"whatsapp-sender/config"
	"whatsapp-sender/constants"
	"whatsapp-sender/db"
	"whatsapp-sender/handler"
	"whatsapp-sender/handler/templates"
	"whatsapp-sender/models"
	"whatsapp-sender/redis"
	"whatsapp-sender/utils"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// create a websocket server

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func wsHandler(c *gin.Context) {
	w := c.Writer
	r := c.Request

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		c.JSON(400, gin.H{
			"message": "Error in upgrading connection " + err.Error(),
		})
		return
	}

	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}

		if string(msg) == "ping" {
			conn.WriteMessage(msgType, []byte("pong"))
			continue
		}

		if string(msg) == "close" {
			conn.Close()
			return
		}

		conn.WriteMessage(msgType, msg)
	}
}

func main() {
	// Create a new instance of the server

	// Load environment variables
	config.LoadEnv()

	// gin.SetMode(gin.ReleaseMode)

	db.ConnectDB()
	redis.ConnectToRedis()

	db.RegisterModels(constants.WhatsappTemplateCollection, constants.MessageLogCollection)

	go utils.QueueProcessing()

	// gin.SetMode(gin.ReleaseMode)
	server := gin.Default()

	// Enable CORS
	server.Use(cors.Default())

	server.GET("/", handler.Home)
	server.GET("/app-logger-ws", wsHandler)

	templateRoutes := server.Group("/templates")
	otpRoutes := server.Group("/otp")

	otpRoutes.POST("/send", handler.SendOTP)
	otpRoutes.POST("/validate", handler.ValidateOtp)

	server.GET("/message-logs", handler.ListMessageLog)

	templateRoutes.GET("", templates.GetTemplateList)
	templateRoutes.POST("", templates.CreateTemplate)
	templateRoutes.DELETE(":id", templates.DeleteTemplate)

	templateRoutes.POST("/send-message", func(ctx *gin.Context) {

		var data struct {
			ID        string            `json:"id" binding:"required"`
			Variables map[string]string `json:"variables" binding:"required"`
			Phone     []string          `json:"phone" binding:"required"`
		}

		err := ctx.ShouldBind(&data)

		if err != nil {
			ctx.JSON(400, gin.H{
				"message": "Error in parsing data",
				"error":   err.Error(),
			})
			return
		}

		if len(data.Phone) == 0 {
			ctx.JSON(400, gin.H{
				"message": "Invalid data",
				"error":   "Phone number is required",
			})
			return
		}

		template, err := models.GetTemplateByName(data.ID)

		if err != nil {
			ctx.JSON(500, gin.H{
				"error": err.Error(),
			})
			return
		}

		errorList := models.ValidateVariables(template, data.Variables)

		if len(errorList) != 0 {
			ctx.JSON(400, gin.H{
				"message": "Invalid data",
				"error":   strings.Join(errorList, "\n"),
			})
			return
		}

		result := models.GenerateWhatsappMessage(template, data.Variables)

		utils.QueueMessage(result, data.Phone)

		ctx.JSON(200, gin.H{
			"message": "Message in queue... will be sent soon",
		})

	})

	// Start the server on port 8080
	server.Run(":8081")
}
