package main

import (
	"bytes"
	"container/list"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/joho/godotenv"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type urls struct {
	siteUrls map[string]int
	mu       sync.Mutex
	codeUrls map[int][]string
}

// addUrl метод добавления URL
func (u *urls) addUrl(url string) {
	//u.mu.Lock()
	//defer u.mu.Unlock()
	u.siteUrls[url] = 0
}

func (u *urls) setCodeUrl(url string, code int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.siteUrls[url] = code
	u.codeUrls[code] = append(u.codeUrls[code], url)
}

func (u *urls) isExist(url string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	if _, ok := u.siteUrls[url]; ok {
		return true
	}

	u.addUrl(url)
	queue.PushBack(url)
	return false
}

var site string

var re = regexp.MustCompile(`(?U)<a.*href=(['"])(https://|/)(.*)(/'|/"|'|").*>`)

var MapUrls urls

var queue = list.New()

var TgToken string
var TgChatId string

func main() {
	godotenv.Load()
	TgToken = os.Getenv("TELEGRAM_TOKEN")
	TgChatId = os.Getenv("TELEGRAM_CHAT_ID")
	//получаем сайт для парсинга
	flag.StringVar(&site, "site", "", "Сайт для парсинга")
	flag.Parse()

	if site == "" {
		panic("site is empty")
	}

	startTime := time.Now()
	limit := 10 // не более 2 запросов в секунду
	ticker := time.NewTicker(time.Second / time.Duration(limit))
	defer ticker.Stop()

	sem := make(chan struct{}, limit) // ограничение на одновременные горутины

	MapUrls = urls{
		siteUrls: make(map[string]int),
		mu:       sync.Mutex{},
		codeUrls: make(map[int][]string),
	}

	MapUrls.addUrl(site)
	parse(site)

	for queue.Len() > 0 {
		page := queue.Front()
		queue.Remove(page)
		fmt.Println("Страниц всего:", len(MapUrls.siteUrls), "Текущая очередь:", queue.Len())

		<-ticker.C        // ждём разрешения от тикера
		sem <- struct{}{} // резервируем место в канале (если он заполнен — ждём)

		go func(page *list.Element) {
			defer func() { <-sem }() // освобождаем слот после завершения
			parse(page.Value.(string))
		}(page)

		fmt.Println("Страниц всего:", len(MapUrls.siteUrls), "Текущая очередь:", queue.Len())
	}

	writeResultFile()

	fmt.Println("Количество страниц", len(MapUrls.siteUrls))
	fmt.Printf("%.2f minutes", time.Since(startTime).Minutes())
}

func parse(keyList string) {
	key := fmt.Sprint(keyList)
	var urlPre string
	if strings.Contains(key, site) {
		urlPre = "https://" + key
	} else {
		urlPre = "https://" + site + "/" + key
	}

	req, err := http.NewRequest("GET", urlPre, nil)
	if err != nil {
		log.Println("Line 117\n" + err.Error())
		return
	}
	req.Header.Set("User-Agent", "TargetPlusParser")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Line 123", err.Error())
		MapUrls.setCodeUrl(key, 500)
		get, err := http.Get("https://api.telegram.org/bot" + TgToken + "/sendMessage?chat_id=" + TgChatId + "&text=" + url.QueryEscape(urlPre+" - "+err.Error()))
		if err != nil {
			log.Println("Line 127\n" + err.Error())
			return
		}
		get.Body.Close()
		return
	}
	defer res.Body.Close()

	MapUrls.setCodeUrl(key, res.StatusCode)

	if res.StatusCode != 200 {
		log.Println(urlPre, res.StatusCode)
		get, err := http.Get("https://api.telegram.org/bot" + TgToken + "/sendMessage?chat_id=" + TgChatId + "&text=" + url.QueryEscape(urlPre+" : "+strconv.Itoa(res.StatusCode)))
		if err != nil {
			log.Println("Line 145\n" + err.Error())
			return
		}
		get.Body.Close()
		return
	}

	body, _ := io.ReadAll(res.Body)
	for _, match := range re.FindAllStringSubmatch(string(body), -1) {
		if match[3] != "" {
			if !MapUrls.isExist(match[3]) {
			}
		}
	}

	return
}

func writeResultFile() {
	file, err := os.OpenFile(site+"_"+time.Now().Format(time.DateTime)+".json", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic("Создание файла\n" + err.Error())
	}
	defer file.Close()

	marshal, err := json.MarshalIndent(MapUrls.codeUrls, "", "  ")
	if err != nil {
		log.Println(err)
		return
	}
	_, err = file.Write(marshal)
	if err != nil {
		log.Println(err)
		return
	}

	err = sendJSONToTelegram(file.Name(), marshal)
	if err != nil {
		log.Println(err)
		return
	}
}

func sendJSONToTelegram(fileName string, jsonData []byte) error {
	// Создаём буфер для тела запроса
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Добавляем файл в форму
	part, err := writer.CreateFormFile("document", fileName)
	if err != nil {
		return fmt.Errorf("ошибка при создании части формы: %w", err)
	}

	// Записываем JSON-данные напрямую
	_, err = part.Write(jsonData)
	if err != nil {
		return fmt.Errorf("ошибка при записи JSON: %w", err)
	}

	// Добавляем chat_id
	_ = writer.WriteField("chat_id", TgChatId)

	// Закрываем writer
	err = writer.Close()
	if err != nil {
		return fmt.Errorf("ошибка при закрытии writer: %w", err)
	}

	// Создаём HTTP-запрос
	urlTG := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", TgToken)
	req, err := http.NewRequest("POST", urlTG, &requestBody)
	if err != nil {
		return fmt.Errorf("ошибка при создании запроса: %w", err)
	}

	// Устанавливаем заголовки
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Отправляем запрос
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка при выполнении запроса: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ошибка отправки файла, статус: %s", resp.Status)
	}

	return nil
}
