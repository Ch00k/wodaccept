package main

import (
	"bytes"
	"errors"
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
	"github.com/bdenning/go-pushover"
	"github.com/radovskyb/watcher"
	"golang.org/x/net/html"
)

// const
var (
	subjectSubstring   = "open for reservation"
	urlRegexp          = regexp.MustCompile(`.*(http://mandrillapp.*)">Accept.*`)
	classDetailsRegexp = regexp.MustCompile(`Date:\xa0+(\d{2}), (.*) (\d{4})Start time:\xa0+(.*) at (.*)End.*Program:\xa0+(.*)Location.*`)

	title   = "//div[@id='AthleteTheme_wt12_block_wtTitle']"
	content = "//div[@id='AthleteTheme_wt12_block_wtMainContent']"

	timeFormat = "Monday, January 2, 2006, 15:04"
)

var pushoverUser, pushoverToken string

type class struct {
	program string
	time    time.Time
}

type reservationResult struct {
	status string
	class  class
}

func readMessage(f string) (*mail.Message, error) {
	message, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	r := bytes.NewReader(message)
	m, err := mail.ReadMessage(r)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func isReservationOpenMessage(m mail.Message) bool {
	return strings.Contains(m.Header.Get("Subject"), subjectSubstring)
}

func findURL(m mail.Message) (string, error) {
	r := quotedprintable.NewReader(m.Body)
	message, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	match := urlRegexp.FindSubmatch(message)
	if match == nil {
		return "", errors.New("URL not found")
	}

	return string(match[1]), nil
}

func fetchPage(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

func acceptReservation(url string) (*reservationResult, error) {
	page, err := fetchPage(url)
	if err != nil {
		return nil, err
	}

	defer page.Close()

	doc, err := html.Parse(page)
	if err != nil {
		return nil, err
	}

	statusNode := htmlquery.FindOne(doc, title)
	if statusNode == nil {
		return nil, errors.New("Class status node not found")
	}

	detailsNode := htmlquery.FindOne(doc, content)
	if detailsNode == nil {
		return nil, errors.New("Class details node not found")
	}

	s := htmlquery.InnerText(statusNode)

	m := classDetailsRegexp.FindStringSubmatch(htmlquery.InnerText(detailsNode))
	if m == nil {
		return nil, errors.New("Class details not found")
	}

	day, month, year, weekDay, timeStr, program := m[1], m[2], m[3], m[4], m[5], m[6]
	t, err := time.Parse(timeFormat, fmt.Sprintf("%s, %s %s, %s, %s", weekDay, month, day, year, timeStr))
	if err != nil {
		return nil, err
	}

	c := class{program: program, time: t}

	return &reservationResult{status: s, class: c}, nil
}

func sendNotification(text string) {
	m := pushover.NewMessage(pushoverToken, pushoverUser)

	log.Println("Sending notification")
	r, err := m.Push(text)
	if err != nil {
		log.Println(err)
	} else {
		log.Printf("Notification sent. Response: %s", r)
	}
}

func main() {
	watchDir := os.Args[1]
	pushoverToken = os.Args[2]
	pushoverUser = os.Args[3]

	w := watcher.New()

	go func() {
		for {
			select {
			case event := <-w.Event:
				if event.Op != watcher.Create {
					continue
				}

				filePath := path.Join(watchDir, event.Name())
				log.Println(filePath)

				m, err := readMessage(filePath)
				if err != nil {
					log.Println(err)
					sendNotification(err.Error())
					continue
				}

				if !isReservationOpenMessage(*m) {
					msg := "Not an 'open for reservation' message"
					log.Println(msg)
					sendNotification(msg)
					continue
				}

				url, err := findURL(*m)
				if err != nil {
					log.Println(err)
					sendNotification(err.Error())
					continue
				}

				r, err := acceptReservation(url)
				if err != nil {
					log.Println(err)
					sendNotification(fmt.Sprintf("%s\n%s", err.Error(), url))
					continue
				}

				text := fmt.Sprintf("%s (%s, %s)", r.status, r.class.program, r.class.time)
				log.Println(text)
				sendNotification(text)
			case err := <-w.Error:
				log.Println(err)
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
