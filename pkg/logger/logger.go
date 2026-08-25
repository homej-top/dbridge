package logger

import (
	"os"

	"github.com/dbridge/dbridge/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func Init(cfg config.LogConfig) (*zap.Logger, error) {
	var level zapcore.Level
	switch cfg.Level {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var core zapcore.Core

	if cfg.Format == "json" {
		encoder := zapcore.NewJSONEncoder(encoderConfig)
		if cfg.Output == "file" {
			f, err := os.OpenFile(cfg.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				return nil, err
			}
			core = zapcore.NewCore(encoder, zapcore.AddSync(f), level)
		} else {
			core = zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)
		}
	} else {
		encoder := zapcore.NewConsoleEncoder(encoderConfig)
		core = zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)
	}

	logger := zap.New(core, zap.AddCaller())
	return logger, nil
}
