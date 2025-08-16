package main

import (
	monitor2 "awesomeProject/monitor"
	"context"
	"errors"
	"log"
	"strings"

	"github.com/spf13/viper"
)

func initConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/etc/docker-log-monitor/")

	viper.SetDefault("webhook_url", "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK")
	viper.SetDefault("log_levels", []string{"ERROR", "FATAL", "PANIC", "WARN"})
	viper.SetDefault("container_filter", []string{})
	viper.SetDefault("check_interval", 30)
	viper.SetDefault("restart_delay", 2)

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetEnvPrefix("DOCKER_MONITOR")

	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			log.Println("📄 No config file found, using defaults and environment variables")
		}
	} else {
		log.Printf("📄 Using config file: %s", viper.ConfigFileUsed())
	}
}

func loadConfig() monitor2.Config {
	return monitor2.Config{
		WebhookURL:      viper.GetString("webhook_url"),
		LogLevels:       viper.GetStringSlice("log_levels"),
		ContainerFilter: viper.GetStringSlice("container_filter"),
		CheckInterval:   viper.GetInt("check_interval"),
		RestartDelay:    viper.GetInt("restart_delay"),
	}
}

func validateConfig(config monitor2.Config) error {
	if config.WebhookURL == "" || config.WebhookURL == "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK" {
		log.Println("⚠️  Warning: Please set a valid webhook URL")
		log.Println("   Set DOCKER_MONITOR_WEBHOOK_URL environment variable or")
		log.Println("   Create a config.yaml file with webhook_url")
	}
	if len(config.LogLevels) == 0 {
		log.Println("⚠️  Warning: No log levels specified, using defaults")
		config.LogLevels = []string{"ERROR", "FATAL", "PANIC"}
	}
	return nil
}

func printConfig(config monitor2.Config) {
	log.Println("📋 Current Configuration:")
	log.Printf("   Webhook URL: %s", maskWebhookURL(config.WebhookURL))
	log.Printf("   Log Levels: %v", config.LogLevels)
	log.Printf("   Container Filter: %s", getFilterDisplay(config.ContainerFilter))
	log.Printf("   Check Interval: %d seconds", config.CheckInterval)
	log.Printf("   Restart Delay: %d seconds", config.RestartDelay)
}

func maskWebhookURL(url string) string {
	if url == "" || url == "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK" {
		return url
	}
	if len(url) > 50 {
		return url[:30] + "***" + url[len(url)-10:]
	}
	return "***configured***"
}

func getFilterDisplay(filter []string) string {
	filterDisplay := strings.Builder{}
	for _, v := range filter {
		filterDisplay.WriteString(v + "\n")
	}
	return filterDisplay.String()
}

func main() {
	log.Println("Kapstra Agent with Viper Config")
	initConfig()
	config := loadConfig()
	if err := validateConfig(config); err != nil {
		log.Fatal("Configuration validation failed:", err)
	}
	printConfig(config)

	monitor, err := monitor2.NewLogMonitor(config)
	if err != nil {
		log.Fatal("Failed to create log monitor:", err)
	}

	ctx := context.Background()
	if err := monitor.Start(ctx); err != nil {
		log.Fatal("Monitor failed:", err)
	}
}
