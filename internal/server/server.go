package server

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"wakego/internal/config"
	"wakego/internal/wol"

	"github.com/gin-gonic/gin"
)

//go:embed web/index.html
var webFiles embed.FS

type Options struct {
	Store  *config.Store
	Logger *log.Logger
}

type Server struct {
	store  *config.Store
	logger *log.Logger
	index  *template.Template
}

type jsonResponse struct {
	OK      bool        `json:"ok"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type wakeRequest struct {
	ID string `json:"id"`
}

type saveDeviceRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MAC       string `json:"mac"`
	Broadcast string `json:"broadcast"`
	Port      int    `json:"port"`
	Remark    string `json:"remark"`
}

type deleteDeviceRequest struct {
	ID string `json:"id"`
}

type saveConfigRequest struct {
	Title         string `json:"title"`
	AdminPassword string `json:"admin_password"`
	DefaultPort   int    `json:"default_port"`
}

func New(opts Options) (*gin.Engine, error) {
	if opts.Store == nil {
		return nil, errors.New("store is required")
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}

	indexBytes, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		return nil, fmt.Errorf("read web asset: %w", err)
	}
	index, err := template.New("index").Parse(string(indexBytes))
	if err != nil {
		return nil, fmt.Errorf("parse web asset: %w", err)
	}

	s := &Server{
		store:  opts.Store,
		logger: opts.Logger,
		index:  index,
	}

	r := gin.New()
	r.Use(s.withLogging())
	r.Use(gin.Recovery())

	r.GET("/", s.handleIndex)
	r.GET("/api/devices", s.handleListDevices)
	r.POST("/api/wake", s.handleWake)
	r.GET("/api/admin/config", s.withAdmin(), s.handleAdminConfig)
	r.POST("/api/admin/config/save", s.withAdmin(), s.handleSaveConfig)
	r.POST("/api/admin/device/save", s.withAdmin(), s.handleSaveDevice)
	r.POST("/api/admin/device/delete", s.withAdmin(), s.handleDeleteDevice)

	return r, nil
}

func (s *Server) handleIndex(c *gin.Context) {
	data := struct {
		Title string
	}{
		Title: s.store.Snapshot().Title,
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := s.index.Execute(c.Writer, data); err != nil {
		s.logger.Printf("render index: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
	}
}

func (s *Server) handleListDevices(c *gin.Context) {
	cfg := s.store.Snapshot()
	c.JSON(http.StatusOK, jsonResponse{
		OK: true,
		Data: map[string]interface{}{
			"title":        cfg.Title,
			"default_port": cfg.DefaultPort,
			"devices":      cfg.Devices,
		},
	})
}

func (s *Server) handleWake(c *gin.Context) {
	var req wakeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{OK: false, Message: err.Error()})
		return
	}

	device, ok := s.store.GetDevice(req.ID)
	if !ok {
		c.JSON(http.StatusNotFound, jsonResponse{OK: false, Message: "device not found"})
		return
	}

	if err := wol.Send(device.MAC, device.Broadcast, device.Port); err != nil {
		s.logger.Printf("wake %s: %v", device.ID, err)
		c.JSON(http.StatusBadGateway, jsonResponse{OK: false, Message: "发送魔术包失败: " + err.Error()})
		return
	}
	s.logger.Printf("wake sent device=%s name=%q broadcast=%s port=%d", device.ID, device.Name, device.Broadcast, device.Port)

	c.JSON(http.StatusOK, jsonResponse{
		OK:      true,
		Message: "已发送唤醒指令",
		Data:    device,
	})
}

func (s *Server) handleAdminConfig(c *gin.Context) {
	cfg := s.store.Snapshot()
	c.JSON(http.StatusOK, jsonResponse{
		OK: true,
		Data: map[string]interface{}{
			"title":        cfg.Title,
			"default_port": cfg.DefaultPort,
		},
	})
}

func (s *Server) handleSaveConfig(c *gin.Context) {
	var req saveConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{OK: false, Message: err.Error()})
		return
	}

	cfg, err := s.store.SaveSettings(config.Config{
		Title:         req.Title,
		AdminPassword: req.AdminPassword,
		DefaultPort:   req.DefaultPort,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{OK: false, Message: err.Error()})
		return
	}
	s.logger.Printf("config updated title=%q default_port=%d", cfg.Title, cfg.DefaultPort)

	c.JSON(http.StatusOK, jsonResponse{
		OK:      true,
		Message: "配置已保存",
		Data: map[string]interface{}{
			"title":        cfg.Title,
			"default_port": cfg.DefaultPort,
		},
	})
}

func (s *Server) handleSaveDevice(c *gin.Context) {
	var req saveDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{OK: false, Message: err.Error()})
		return
	}

	port := req.Port
	if port == 0 {
		port = s.store.Snapshot().DefaultPort
	}

	device, err := s.store.SaveDevice(config.Device{
		ID:        req.ID,
		Name:      req.Name,
		MAC:       req.MAC,
		Broadcast: req.Broadcast,
		Port:      port,
		Remark:    req.Remark,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{OK: false, Message: err.Error()})
		return
	}
	s.logger.Printf("device saved id=%s name=%q broadcast=%s port=%d", device.ID, device.Name, device.Broadcast, device.Port)

	c.JSON(http.StatusOK, jsonResponse{OK: true, Message: "设备已保存", Data: device})
}

func (s *Server) handleDeleteDevice(c *gin.Context) {
	var req deleteDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{OK: false, Message: err.Error()})
		return
	}

	if err := s.store.DeleteDevice(req.ID); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{OK: false, Message: err.Error()})
		return
	}
	s.logger.Printf("device deleted id=%s", req.ID)

	c.JSON(http.StatusOK, jsonResponse{OK: true, Message: "设备已删除"})
}

func (s *Server) withAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		password := strings.TrimSpace(c.GetHeader("X-Admin-Password"))
		if password == "" {
			password = strings.TrimSpace(c.Query("password"))
		}
		if password == "" {
			s.logger.Printf("admin auth failed path=%s reason=missing_password remote=%s", c.Request.URL.Path, c.ClientIP())
			c.JSON(http.StatusUnauthorized, jsonResponse{OK: false, Message: "missing admin password"})
			c.Abort()
			return
		}
		if password != s.store.Snapshot().AdminPassword {
			s.logger.Printf("admin auth failed path=%s reason=invalid_password remote=%s", c.Request.URL.Path, c.ClientIP())
			c.JSON(http.StatusUnauthorized, jsonResponse{OK: false, Message: "invalid admin password"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) withLogging() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf(
			"access remote=%s method=%s path=%s status=%d bytes=%d duration=%s\n",
			param.ClientIP,
			param.Method,
			param.Path,
			param.StatusCode,
			param.BodySize,
			param.Latency.Round(time.Millisecond),
		)
	})
}
