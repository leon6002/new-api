package common

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// 定义全局命令行标志。
var (
	Port         = flag.Int("port", 3001, "the listening port")                  // 监听端口
	PrintVersion = flag.Bool("version", false, "print version and exit")         // 打印版本信息并退出
	PrintHelp    = flag.Bool("help", false, "print help and exit")               // 打印帮助信息并退出
	LogDir       = flag.String("log-dir", "./logs", "specify the log directory") // 指定日志目录
)

// printHelp 打印帮助信息。
func printHelp() {
	fmt.Println("New API " + Version + " - All in one API service for OpenAI API.")
	fmt.Println("Copyright (C) 2023 JustSong. All rights reserved.")
	fmt.Println("GitHub: https://github.com/songquanpeng/one-api")
	fmt.Println("Usage: one-api [--port <port>] [--log-dir <log directory>] [--version] [--help]")
}

// init 初始化程序，处理命令行参数。
func init() {
	flag.Parse()

	// 处理打印版本信息的请求。
	if *PrintVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	// 处理打印帮助信息的请求。
	if *PrintHelp {
		printHelp()
		os.Exit(0)
	}

	// 检查环境变量 SESSION_SECRET，提供默认值并警告。
	if os.Getenv("SESSION_SECRET") != "" {
		ss := os.Getenv("SESSION_SECRET")
		if ss == "random_string" {
			log.Println("WARNING: SESSION_SECRET is set to the default value 'random_string', please change it to a random string.")
			log.Println("警告：SESSION_SECRET被设置为默认值'random_string'，请修改为随机字符串。")
			log.Fatal("Please set SESSION_SECRET to a random string.")
		} else {
			SessionSecret = ss
		}
	}

	// 处理 SQLite 数据库路径的环境变量。
	if os.Getenv("SQLITE_PATH") != "" {
		SQLitePath = os.Getenv("SQLITE_PATH")
	}

	// 处理日志目录，确保目录存在。
	if *LogDir != "" {
		var err error
		*LogDir, err = filepath.Abs(*LogDir)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stat(*LogDir); os.IsNotExist(err) {
			err = os.Mkdir(*LogDir, 0777)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}
