package service

import (
	"bytes"
	"fmt"
	"github.com/anknown/ahocorasick"
	"one-api/constant"
	"strings"
)

// SensitiveWordContains 是否包含敏感词，返回是否包含敏感词和敏感词列表
func SensitiveWordContains(text string) (bool, []string) {
	if len(constant.SensitiveWords) == 0 {
		return false, nil
	}
	checkText := strings.ToLower(text)
	// 构建一个AC自动机
	m := initAc()
	hits := m.MultiPatternSearch([]rune(checkText), false)
	if len(hits) > 0 {
		words := make([]string, 0)
		for _, hit := range hits {
			words = append(words, string(hit.Word))
		}
		return true, words
	}
	return false, nil
}

// SensitiveWordReplace 接收一个字符串和一个布尔值作为参数，用于敏感词替换。
// 如果敏感词列表为空，则返回 false、nil 和原始文本。
// 将输入文本转换为小写，并初始化敏感词自动机。
// 使用自动机进行多模式搜索，返回所有匹配的敏感词。
// 如果有匹配的敏感词，则将其替换为 "*###*"，并返回 true、敏感词列表和替换后的文本。
// 如果没有匹配的敏感词，则返回 false、nil 和原始文本。
func SensitiveWordReplace(text string, returnImmediately bool) (bool, []string, string) {
	if len(constant.SensitiveWords) == 0 {
		return false, nil, text
	}
	checkText := strings.ToLower(text)
	m := initAc()
	hits := m.MultiPatternSearch([]rune(checkText), returnImmediately)
	if len(hits) > 0 {
		words := make([]string, 0)
		for _, hit := range hits {
			pos := hit.Pos
			word := string(hit.Word)
			text = text[:pos] + "*###*" + text[pos+len(word):]
			words = append(words, word)
		}
		return true, words, text
	}
	return false, nil, text
}

func initAc() *goahocorasick.Machine {
	m := new(goahocorasick.Machine)
	dict := readRunes()
	if err := m.Build(dict); err != nil {
		fmt.Println(err)
		return nil
	}
	return m
}

func readRunes() [][]rune {
	var dict [][]rune

	for _, word := range constant.SensitiveWords {
		word = strings.ToLower(word)
		l := bytes.TrimSpace([]byte(word))
		dict = append(dict, bytes.Runes(l))
	}

	return dict
}
