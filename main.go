package main

import (
	"container/list"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
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
}

func (u *urls) addUrl(url string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.siteUrls[url] = 0
}

func (u *urls) setCodeUrl(url string, code int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.siteUrls[url] = code
}

var site string

var re = regexp.MustCompile(`(?U)<a.*href=(['"])(https://|/)(.*)(/'|/"|'|").*>`)

var MapUrls = &urls{
	siteUrls: make(map[string]int),
	mu:       sync.Mutex{},
}

var queue = list.New()

func main() {
	flag.StringVar(&site, "site", "", "Сайт для парсинга")
	flag.Parse()
	if site == "" {
		panic("site is empty")
	}

	startTime := time.Now()
	MapUrls.addUrl(site)
	queue.PushBack(site)

	for queue.Len() > 0 {
		fmt.Println("Текущая очередь:", queue.Len())
		fmt.Println("Прошло времени:", time.Since(startTime).Minutes())
		fmt.Println()
		page := queue.Front()

		fmt.Printf("Start parse: %v\n", page.Value)
		pageTime := time.Now()

		go parse(page)

		fmt.Printf("End parse page - Time: %f\n\n", time.Since(pageTime).Seconds())
	}

	writeResultFile()

	fmt.Println(len(MapUrls.siteUrls))
	fmt.Println(time.Since(startTime))
}

func parse(keyList *list.Element) {
	defer queue.Remove(keyList)

	key := fmt.Sprint(keyList.Value)
	var urlPre string
	if strings.Contains(key, site) {
		urlPre = "https://" + key
	} else {
		urlPre = "https://" + site + "/" + key
	}
	res, err := http.Get(urlPre)
	if err != nil {
		panic("Line 61\n" + err.Error())
	}
	defer res.Body.Close()

	MapUrls.setCodeUrl(key, res.StatusCode)

	if res.StatusCode != 200 {
		log.Println(urlPre, res.StatusCode)
		return
	}

	body, _ := io.ReadAll(res.Body)
	for _, match := range re.FindAllStringSubmatch(string(body), -1) {
		if match[3] != "" {
			if _, ok := MapUrls.siteUrls[match[3]]; !ok {
				MapUrls.addUrl(match[3])
				queue.PushBack(match[3])
			}
			// TODO этот else чтобы узнать количество ссылок на сайте
			//else if ok {
			//siteUrls[match[3]]++
			//}
		}
	}
}

func writeResultFile() {
	var siteUrlsStr string
	//TODO нужен отдельный файл для страниц где код ответа не 200
	for key, code := range MapUrls.siteUrls {
		siteUrlsStr += key + "=" + strconv.Itoa(code) + "\n"
	}

	file, err := os.OpenFile(site+".html", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic("Создание файла\n" + err.Error())
	}
	defer file.Close()

	siteUrlsStr = strings.TrimRight(siteUrlsStr, "\n")
	_, err = file.WriteString(siteUrlsStr)
	if err != nil {
		panic("Запись файла\n" + err.Error())
	}
}
