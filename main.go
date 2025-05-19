package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Config 配置结构体
type Config struct {
	TargetDir        string   `json:"target_dir"`
	OutputDir        string   `json:"output_dir"`
	ReplacementsFile string   `json:"replacements_file"`
	FileExtensions   []string `json:"file_extensions"`
	IgnoreReadOnly   bool     `json:"ignore_read_only"`
	IncludeSubDirs   bool     `json:"include_sub_dirs"`
}

func main() {
	// 命令行参数
	configPath := flag.String("config", "config.json", "Path to config.json file")
	flag.Parse()

	// 初始化日志
	logDir := "logs"
	err := os.MkdirAll(logDir, os.ModePerm)
	if err != nil {
		fmt.Println("无法创建日志目录:", err)
		return
	}
	logFileName := filepath.Join(logDir, "replace_"+time.Now().Format("20060102_150405")+".log")
	logFile, err := os.Create(logFileName)
	if err != nil {
		fmt.Println("无法创建日志文件:", err)
		return
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	// 加载配置
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 加载替换规则
	replacements, err := loadReplacements(cfg.ReplacementsFile)
	if err != nil {
		log.Fatalf("加载替换文件失败: %v", err)
	}

	// 遍历目标目录处理文件
	err = filepath.Walk(cfg.TargetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("访问路径失败: %s 错误: %v", path, err)
			return nil
		}
		if info.IsDir() {
			if path != cfg.TargetDir && !cfg.IncludeSubDirs {
				return filepath.SkipDir
			}
			if cfg.IgnoreReadOnly && isReadOnly(info) {
				log.Printf("跳过只读目录: %s", path)
				return filepath.SkipDir
			}
			return nil
		}

		if len(cfg.FileExtensions) > 0 && !hasValidExtension(info.Name(), cfg.FileExtensions) {
			return nil
		}

		relPath, err := filepath.Rel(cfg.TargetDir, path)
		if err != nil {
			log.Printf("路径转换失败: %v", err)
			return nil
		}
		dstPath := filepath.Join(cfg.OutputDir, relPath)

		changed, err := replaceInFileWithEncodingAutoDetect(path, dstPath, replacements)
		if err != nil {
			log.Printf("替换失败: %s 错误: %v", path, err)
		} else if changed {
			log.Printf("替换完成: %s", path)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("目录遍历失败: %v", err)
	}

	log.Println("所有文件替换处理完成。")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	return &cfg, err
}

func isReadOnly(info os.FileInfo) bool {
	return info.Mode().Perm()&0200 == 0
}

func hasValidExtension(name string, exts []string) bool {
	for _, ext := range exts {
		if strings.HasSuffix(strings.ToLower(name), strings.ToLower(ext)) {
			return true
		}
	}
	return false
}

// 读取替换规则，格式：{key} = {value}
func loadReplacements(file string) (map[string]string, error) {
	result := make(map[string]string)

	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "}={") {
			parts := strings.SplitN(line, "}={", 2)
			if len(parts) == 2 {
				key := strings.Trim(parts[0], " {}")
				val := strings.Trim(parts[1], " {}")
				if key != "" && val != "" {
					result[key] = val
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// 自动识别编码读文件，替换内容后写回（保持原编码）
func replaceInFileWithEncodingAutoDetect(srcPath, dstPath string, replacements map[string]string) (bool, error) {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return false, err
	}

	var content string
	isGBK := false

	if utf8.Valid(raw) {
		content = string(raw)
	} else {
		reader := transform.NewReader(bytes.NewReader(raw), simplifiedchinese.GBK.NewDecoder())
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return false, err
		}
		content = string(decoded)
		isGBK = true
	}

	original := content
	content = applyReplacements(content, replacements)
	if content == original {
		return false, nil
	}

	err = os.MkdirAll(filepath.Dir(dstPath), os.ModePerm)
	if err != nil {
		return false, err
	}

	var outBytes []byte
	if isGBK {
		var buf bytes.Buffer
		writer := transform.NewWriter(&buf, simplifiedchinese.GBK.NewEncoder())
		_, err = writer.Write([]byte(content))
		if err != nil {
			return false, err
		}
		writer.Close()
		outBytes = buf.Bytes()
	} else {
		outBytes = []byte(content)
	}

	err = os.WriteFile(dstPath, outBytes, 0644)
	return err == nil, err
}

// 使用 strings.ReplaceAll 完全匹配替换，不使用正则，防止误替换
func applyReplacements(content string, replacements map[string]string) string {
	for old, newVal := range replacements {
		if strings.Contains(content, old) {
			content = strings.ReplaceAll(content, old, newVal)
		}
	}
	return content
}
