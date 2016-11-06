package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/antchfx/xquery/html"
	"github.com/radovskyb/watcher"
	"golang.org/x/net/html"
)

const (
	brokenLineRegexp = `.*=[^3D].*`
	subjRegexp       = `The .* class is open for reservation`
	urlRegexp        = `.*(http://mandrillapp.*)">Accept.*`
	classInfoRegexp  = `Date:\xa0+(\d{2}), (.*) (\d{4})Start time:\xa0+(.*) at (.*)End.*Program:\xa0+(.*)Location.*`

	title   = "//div[@id='W_Theme_UI_wt12_block_wtTitle']"
	content = "//div[@id='W_Theme_UI_wt12_block_wtMainContent']"
)

func checkSubject(h mail.Header) (bool, error) {
	s := h.Get("Subject")
	m, err := regexp.MatchString(subjRegexp, s)
	if err != nil {
		return false, err
	}
	return m, err
}

func fixBody(r io.Reader) (io.Reader, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		match, err := regexp.MatchString(brokenLineRegexp, line)
		if err != nil {
			return nil, err
		}
		if match {
			lines[i] = "FOO"
		}
	}

	output := strings.Join(lines, "\n")
	return strings.NewReader(output), err
}

func decodeBody(r io.Reader) ([]byte, error) {
	qr := quotedprintable.NewReader(r)
	return ioutil.ReadAll(qr)
}

func findURL(b []byte) string {
	lines := strings.Split(string(b), "\n")
	r := regexp.MustCompile(urlRegexp)

	var url string
	for _, l := range lines {
		m := r.FindStringSubmatch(l)
		if m != nil {
			url = m[1]
		}
	}
	return url
}

func readEmail(f string) string {
	msg, err := ioutil.ReadFile(f)
	if err != nil {
		log.Fatal(err)
	}

	r := bytes.NewReader(msg)
	m, err := mail.ReadMessage(r)
	if err != nil {
		log.Fatal(err)
	}

	match, err := checkSubject(m.Header)
	if err != nil {
		log.Fatal(err)
	}
	if !match {
		return ""
	}

	b, err := fixBody(m.Body)
	if err != nil {
		log.Fatal(err)
	}

	db, err := decodeBody(b)
	if err != nil {
		log.Fatal(err)
	}

	url := findURL(db)
	if url != "" {
		return url
	}
	return ""
}

func fetchURL(url string) string {
	if url == "" {
		return ""
	}
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	return string(body)
}

func parseHTML(body string) {
	if body == "" {
		return
	}
	root, _ := html.Parse(strings.NewReader(body))
	status := htmlquery.FindOne(root, title)
	info := htmlquery.FindOne(root, content)

	r := regexp.MustCompile(classInfoRegexp)
	m := r.FindStringSubmatch(htmlquery.InnerText(info))
	if m == nil {
		log.Println(htmlquery.InnerText(status))
	} else {
		day := m[1]
		month := m[2]
		year := m[3]
		weekDay := m[4]
		time := m[5]
		class := m[6]
		date := fmt.Sprintf("%s, %s %s, %s", weekDay, month, day, year)
		log.Println(htmlquery.InnerText(status), time, date, class)
	}
}

func main() {
	watchDir := os.Args[1]
	w := watcher.New()

	go func() {
		for {
			select {
			case event := <-w.Event:
				if event.Op != watcher.Create {
					continue
				}
				filePath := path.Join(watchDir, event.Name())
				url := readEmail(filePath)
				body := fetchURL(url)
				parseHTML(body)
			case err := <-w.Error:
				log.Fatal(err)
			}
		}
	}()

	if err := w.Add(watchDir); err != nil {
		log.Fatal(err)
	}

	if err := w.Start(time.Millisecond * 100); err != nil {
		log.Fatal(err)
	}
}
