package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/DataDog/datadog-go/statsd"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	ginlogrus "github.com/toorop/gin-logrus"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

const (
	defaultDogStatsDHost = "127.0.0.1"
	defaultDogStatsDPort = "8125"
	defaultMetricPrefix  = "sendgrid.event."
	defaultServerPort    = "8080"
)

var (
	sendgridAPIKey       string
	sendgridClientSecret string
	sendgridClientID     string
	rdb                  *redis.Client
	metricPrefix         string
	statsdClient         *statsd.Client
)

// SendGridEvents represents the scheme of Event Webhook body
// https://sendgrid.com/docs/API_Reference/Webhooks/event.html#-Event-POST-Example
type SendGridEvents []struct {
	SGMessageID string   `json:"sg_message_id"`
	Email       string   `json:"email"`
	Timestamp   int      `json:"timestamp"`
	SMTPID      string   `json:"smtp-id,omitempty"`
	Event       string   `json:"event"`
	Category    []string `json:"category,omitempty"`
	URL         string   `json:"url,omitempty"`
	AsmGroupID  int      `json:"asm_group_id,omitempty"`
}

func webhookHandler(c *gin.Context) {
	// authorize request
	reqlogger := logrus.New()
	authHeader := c.Request.Header.Get("Authorization")
	token := strings.Split(authHeader, " ")[1]
	// check redis for token
	redisToken, err := rdb.Get(context.Background(), "oauth_token").Result()
	if err != nil {
		reqlogger.WithField("event", "getting redis token").Errorln(err)
		c.JSON(500, map[string]string{
			"error": "internal server error",
		})
		return
	}
	if token != redisToken {
		reqlogger.WithField("event", "comparing token").Errorf("token supplied: %s, token we have %s", token, redisToken)
		c.JSON(401, map[string]string{
			"error": "unauthorized request",
		})
		return
	}
	bodyBytes, _ := ioutil.ReadAll(c.Request.Body)
	// consider the token verified
	var events SendGridEvents
	reqlogger.Infoln(string(bodyBytes))
	if err := json.Unmarshal(bodyBytes, &events); err != nil {
		reqlogger.WithField("event", "unmarshalling events").Errorln(err)
		c.JSON(500, map[string]string{"error": err.Error()})
		return
	}

	for _, event := range events {
		if err := statsdClient.Incr(metricPrefix+event.Event, nil, 1); err != nil {
			reqlogger.WithField("event", "sending event to datadog statsd client").Errorln(err)
			c.JSON(500, map[string]string{"error": err.Error()})
			return
		}
	}
	c.JSON(200, nil)
}

func healthcheck(c *gin.Context) {
	c.JSON(200, map[string]string{
		"status": "ok",
	})
	return
}

type SendgridOauthResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func oauth(c *gin.Context) {
	// check the key pairing
	clientID, clientSecret, _ := c.Request.BasicAuth()
	if clientID != sendgridClientID || clientSecret != sendgridClientSecret {
		c.JSON(403, nil)
		return
	}
	accessToken := uuid.New().String()
	accessTokenEncoded := base64.StdEncoding.EncodeToString([]byte(accessToken))
	rdb.Set(context.Background(), "oauth_token", accessTokenEncoded, time.Minute*60)
	sor := SendgridOauthResponse{
		AccessToken: accessTokenEncoded,
		TokenType:   "bearer",
		ExpiresIn:   3600,
	}
	c.JSON(200, sor)
}

func main() {
	var (
		dogStatsDHost, dogStatsDPort string
		serverPort                   string
	)
	sendgridAPIKey = os.Getenv("SENDGRID_API_KEY")

	dogStatsDHost = os.Getenv("DOGSTATSD_HOST")
	if dogStatsDHost == "" {
		dogStatsDHost = defaultDogStatsDHost
	}

	dogStatsDPort = os.Getenv("DOGSTATSD_PORT")
	if dogStatsDPort == "" {
		dogStatsDPort = defaultDogStatsDPort
	}

	dogStatsDAddr := fmt.Sprintf("%s:%s", dogStatsDHost, dogStatsDPort)

	metricPrefix = os.Getenv("METRIC_PREFIX")
	if metricPrefix == "" {
		metricPrefix = defaultMetricPrefix
	}

	serverPort = os.Getenv("PORT")
	if serverPort == "" {
		serverPort = defaultServerPort
	}

	sendgridClientSecret = os.Getenv("SENDGRID_CLIENT_SECRET")
	sendgridClientID = os.Getenv("SENDGRID_CLIENT_ID")
	var err error
	log := logrus.New()
	formatter := &logrus.JSONFormatter{
		PrettyPrint: true,
	}
	log.SetReportCaller(true)
	log.SetOutput(os.Stdout)
	log.SetFormatter(formatter)
	rdb = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT")),
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	statsdClient, err = statsd.New(dogStatsDAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	app := gin.Default()
	// panic recovery
	app.Use(gin.Recovery())

	app.Use(ginlogrus.Logger(log), gin.Recovery())
	// default cors
	app.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		AllowCredentials: true,
		MaxAge:           5 * time.Minute,
	}))
	app.Handle("GET", "/healthcheck", healthcheck)
	app.Handle("POST", "/oauth", oauth)
	app.Handle("POST", "/webhook", webhookHandler)
	panic(app.Run("0.0.0.0:8080"))
}
