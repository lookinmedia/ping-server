package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/hashicorp/go-version"
	"golang.org/x/mod/semver"
)

var instance *Watcher
var once sync.Once

func CreateWatcher() *Watcher {
	once.Do(func() {
		instance = newWatcher()
	})
	return instance
}

type Watcher struct {
	mtx         sync.Mutex
	verByServer map[string]*version.Version
}

func newWatcher() *Watcher {
	return &Watcher{verByServer: make(map[string]*version.Version)}
}

func (w *Watcher) Start(ctx context.Context, interval time.Duration) {
	go w.run(ctx, interval)
}

func (w *Watcher) run(ctx context.Context, interval time.Duration) {
	if err := w.update(); err != nil {
		log.Printf("update versions servers failed %s", err)
	}
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ticker.C:
			if err := w.update(); err != nil {
				log.Printf("update versions servers failed %s", err)
				continue
			}
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}
}

func (w *Watcher) VersionByServer(host string) (*version.Version, error) {
	w.mtx.Lock()
	version, ok := w.verByServer[host]
	w.mtx.Unlock()
	if ok {
		return version, nil
	}
	url := "https://misterlauncher.org/servers/search/" + host
	verByServ, err := parsingServersList(url)
	if err != nil {
		return nil, err
	}
	if len(verByServ) == 0 {
		return nil, fmt.Errorf("version not found for host %s", host)
	}
	w.mtx.Lock()
	for host, ver := range verByServ {
		w.verByServer[host] = ver
		version = ver
	}
	w.mtx.Unlock()

	version, err = w.findByContains(host)
	if err != nil {
		return nil, err
	}
	return version, nil
}

func (w *Watcher) update() error {
	lastPage, err := lastPage("https://misterlauncher.org/servers")
	if err != nil {
		return err
	}
	for i := 1; i <= lastPage; i++ {
		url := "https://misterlauncher.org/servers/page/" + strconv.Itoa(i)
		verByServ, err := parsingServersList(url)
		if err != nil {
			log.Printf("failed server versions for page %d err %s", i, err)
			continue
		}
		w.mtx.Lock()
		for server, version := range verByServ {
			w.verByServer[server] = version
		}
		w.mtx.Unlock()
	}
	return nil
}

func (w *Watcher) findByContains(host string) (*version.Version, error) {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	for server, version := range w.verByServer {
		if strings.Contains(server, host) {
			return version, nil
		}
	}
	return nil, fmt.Errorf("version not found for server %s", host)
}

func parsingServersList(url string) (map[string]*version.Version, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Printf("status code error: %d %s", res.StatusCode, res.Status)
		return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
	}
	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}
	versionByServer := make(map[string]*version.Version)
	// Find the items
	var parsErr error
	doc.Find(".servers-list").Find(".server").Each(func(i int, s *goquery.Selection) {
		// For each item found, get the title
		server := s.Find(".info").Find(".ip").Find(".back-tooltip").Find("span").Text()
		ver := s.Find(".info").Find(".block").Text()
		if len(strings.Split(ver, "онлайн")) <= 1 {
			if len(strings.Split(ver, "оффлайн")) <= 1 {
				parsErr = fmt.Errorf("field version not found for server %s", server)
				return
			}
			ver = strings.Split(strings.Split(ver, "оффлайн")[1], "версия")[0]
		} else {
			ver = strings.Split(strings.Split(ver, "онлайн")[1], "версия")[0]
		}
		if !semver.IsValid("v" + ver) {
			parsErr = fmt.Errorf("invalid semver format got version %s", ver)
			return
		}
		if server != "" {
			version, err := version.NewVersion(ver)
			if err != nil || version == nil {
				parsErr = fmt.Errorf("invalid semver format %w got version %s", err, ver)
				return
			}
			versionByServer[server] = version
		}
	})
	return versionByServer, parsErr
}

func lastPage(url string) (int, error) {
	res, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Printf("status code error: %d %s", res.StatusCode, res.Status)
		return -1, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
	}
	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return 0, err
	}
	lastPage := -1
	// Find the review items
	doc.Find(".pagination").Find("li").Find("a").Each(func(i int, s *goquery.Selection) {
		page, err := strconv.Atoi(s.Text())
		if err != nil {
			return
		}
		lastPage = page
	})
	return lastPage, nil
}
