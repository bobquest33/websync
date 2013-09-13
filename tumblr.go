package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mrjones/oauth"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	tumbHost      = "https://api.tumblr.com" //needs to be replaced for testing
	tumbV         = "/v2/"
	tumbFollowing = tumbV + "user/following"
	tumbPosts     = tumbV + "blog/%s/posts?api_key=%s&filter=raw&offset=%d"
)

func Tumblr(f File, files chan File, errs chan error) {
	tumbUri, _ := url.Parse(tumbHost)
	if f.Url.Path != "/" {
		tok, _, err := Keychain(*tumbUri)
		if err != nil {
			errs <- err
			return
		}
		getBlog(f, tok, HClient, files, errs)
	} else {
		tok, _ := OAuth()
		cons := oauth.Consumer{HttpClient: HClient}
		for i := 0; ; i += 20 {
			resp, err := cons.Get(tumbHost+tumbFollowing,
				map[string]string{"offset": strconv.Itoa(i)}, tok)
			check(err)
			var fr followingResponse
			checkResponse(resp, &fr)
			for _, b := range fr.Blogs {
				bUri, err := url.Parse(b.Url)
				check(err)
				bUri.Path = bUri.Host
				files <- File{Url: bUri}
			}
			if i >= fr.Total_blogs {
				break
			}
		}
	}
}

func checkResponse(rc *http.Response, resp interface{}) {
	if !(rc.StatusCode < 300 && rc.StatusCode >= 200) {
		check(errors.New("Request: " + rc.Status))
	}
	data, err := ioutil.ReadAll(rc.Body)
	check(err)
	var cr completeResponse
	err = json.Unmarshal(data, &cr)
	check(err)
	err = json.Unmarshal(cr.Response, &resp)
	if err != nil {
		println("complete response: ", string(cr.Response))
	}
	check(err)
}

func getBlog(fl File, key string, c *http.Client,
	files chan File, errs chan error) {

	for i := int64(0); ; i += 20 {
		thost := fl.Url.Path[1:]
		u := fmt.Sprintf(tumbHost+tumbPosts, thost, key, i)
		println(u)
		r, err := c.Get(u)
		if err != nil {
			errs <- err
			return
		}

		var br blogResponse
		checkResponse(r, &br)

		for _, rawPost := range br.Posts {
			var p post
			err = json.Unmarshal(rawPost, &p)
			if err != nil {
				errs <- err
				return
			}

			fileName := strconv.FormatInt(p.Id, 10)
			mtime := time.Unix(p.Timestamp, 0)

			//store metadata
			files <- File{
				Url:   &url.URL{Path: fl.Url.Path + "/" + fmt.Sprintf(".%s.json", fileName)},
				Mtime: mtime,
				FileFunc: func() (r io.ReadCloser, err error) {
					b, err := json.MarshalIndent(p, "", "\t")
					if err != nil {
						return
					}
					// println(string(b))
					return fakeCloser{bytes.NewReader(b)}, err
				},
			}
			switch p.PostType {
			case "answer", "audio", "chat":
				//not implemented
				continue
			case "link":
				var p linkPost
				err = json.Unmarshal(rawPost, &p)
				if err != nil {
					errs <- err
					return
				}
				files <- File{
					Url: &url.URL{Path: fl.Url.Path + "/" + fmt.Sprintf(
						"%d_link.txt", p.Id)},
					Mtime: mtime,
					FileFunc: func() (r io.ReadCloser, err error) {
						return fakeCloser{strings.NewReader(p.Url)}, nil
					},
				}
			case "quote":
				var p quotePost
				err = json.Unmarshal(rawPost, &p)
				if err != nil {
					errs <- err
					return
				}
				files <- File{
					Url: &url.URL{Path: fl.Url.Path + "/" + fmt.Sprintf(
						"%d_quote.txt", p.Id)},
					Mtime: mtime,
					FileFunc: func() (r io.ReadCloser, err error) {
						return fakeCloser{strings.NewReader(p.Text)}, nil
					},
				}

			case "text":
				var p textPost
				err = json.Unmarshal(rawPost, &p)
				if err != nil {
					errs <- err
					return
				}
				files <- File{
					Url: &url.URL{Path: fl.Url.Path + "/" + fmt.Sprintf(
						"%d.md", p.Id)},
					Mtime: mtime,
					FileFunc: func() (r io.ReadCloser, err error) {
						return fakeCloser{strings.NewReader(p.Body)}, nil
					},
				}

			case "video":
				// println("video source: ", p.Source_url)
				// println("post url: ", p.Post_url)
				// TODO: Fix
				// u, _ := url.Parse(p.)
				// files <- File{Url: u}
				continue
			case "photo":
				var p photoPost
				err = json.Unmarshal(rawPost, &p)
				if err != nil {
					errs <- err
					return
				}

				for i, photo := range p.Photos {
					uri := photo.Alt_sizes[0].Url
					files <- File{
						Url: &url.URL{Path: fl.Url.Path + "/" + fmt.Sprintf(
							"%s-%d.%s", fileName, i, uri[len(uri)-3:])},
						Mtime: mtime,
						FileFunc: func() (
							r io.ReadCloser, err error) {
							resp, err := c.Get(uri)
							if err != nil {
								return nil, err
							} else {
								return resp.Body, nil
							}
						},
					}
				}
				continue
			default:
				errs <- errors.New("Do not know this type")
				return

			}
		}
		if i >= br.Blog.Posts {
			break
		}
	}
	return
}

type fakeCloser struct {
	io.Reader
}

func (f fakeCloser) Close() (err error) {
	return
}

type completeResponse struct {
	Meta     meta
	Response json.RawMessage
}

type meta struct {
	Status int64
	Msg    string
}

type followingResponse struct {
	Total_blogs int
	Blogs       []followingBlog
}

type followingBlog struct {
	Name, Url string
	Updated   int
}

type blogResponse struct {
	Blog  blog
	Posts []json.RawMessage
}

type blog struct {
	Title       string
	Posts       int64
	Name        string
	Url         string
	Updated     int64
	Description string
	Ask         bool
	Ask_anon    bool
}

type post struct {
	Blog_name    string
	Id           int64
	Post_url     string
	PostType     string `json:"type"`
	Timestamp    int64
	Date         string
	Format       string
	Reblog_key   string
	Tags         []string
	Bookmarklet  bool
	Mobile       bool
	Source_url   string
	Source_title string
	Liked        bool
	State        string
	Total_Posts  int64
}

type textPost struct {
	post
	Title, Body string
}

type photoPost struct {
	post
	Photos        []photoObject
	Caption       string
	Width, Height int64
}

type photoObject struct {
	Caption   string
	Alt_sizes []altSize
}

type altSize struct {
	Width, Height int64
	Url           string
}

type quotePost struct {
	post
	Text, Source string
}

type linkPost struct {
	post
	Title, Url, Description string
}

type chatPost struct {
	post
	Title, Body string
	Dialogue    []dialogue
}

type dialogue struct {
	Name, Label, Phrase string
}

type audioPost struct {
	post
	Caption      string
	Player       string
	Plays        int64
	Album_art    string
	Artist       string
	Album        string
	Track_name   string
	Track_number int64
	Year         int64
}

type videoPost struct {
	post
	Caption string
	Player  []player
}

type player struct {
	Width      int64
	Embed_code string
}

type answerPost struct {
	post
	Asking_name string
	Asking_url  string
	Question    string
	Answer      string
}
