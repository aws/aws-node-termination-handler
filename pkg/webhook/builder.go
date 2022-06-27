/*
Copyright 2022 Amazon.com, Inc. or its affiliates. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"fmt"
	"net/http"
	"net/url"
	urlpkg "net/url"
	templatepkg "text/template"
	"time"
)

type (
	HttpSendFunc = func(*http.Request) (*http.Response, error)
	ProxyFunc    = func(*http.Request) (*url.URL, error)

	ClientBuilder func(ProxyFunc) HttpSendFunc
)

func NewHttpClientDo(proxy ProxyFunc) HttpSendFunc {
	c := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			IdleConnTimeout: 1 * time.Second,
			Proxy:           proxy,
		},
	}
	return c.Do
}

func (b ClientBuilder) NewClient(url, proxyURL, template string, headers http.Header) (Client, error) {
	c := Client{}

	if url == "" {
		return c, nil
	}

	proxy := noopProxy
	if proxyURL != "" {
		proxyURL, err := urlpkg.Parse(proxyURL)
		if err != nil {
			return c, fmt.Errorf("failed to parse proxy URL: %w", err)
		}

		proxy = func(_ *http.Request) (*urlpkg.URL, error) {
			return proxyURL, nil
		}
	}

	tmpl, err := templatepkg.New("webhook").Parse(template)
	if err != nil {
		return c, fmt.Errorf("failed to parse template: %w", err)
	}

	c.url = url
	c.headers = headers
	c.sendFunc = b(proxy)
	c.template = tmpl
	return c, nil
}

func noopProxy(_ *http.Request) (*url.URL, error) {
	return nil, nil
}
