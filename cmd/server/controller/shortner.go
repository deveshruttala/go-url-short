package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-batteries/shortner/app/config"
	"github.com/go-batteries/shortner/app/models"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

const (
	AcceptTypeJSON = "application/json"
	AcceptTypeHTML = "text/html"
)

type URLShortner struct {
	keyShardedRepo   *models.URLRepo
	robinShardedRepo *models.URLRepo
	domainName       string
}

func NewURLShortnerCtrl(keyShardedRepo *models.URLRepo, robinShardedRepo *models.URLRepo, domainName string) *URLShortner {
	return &URLShortner{
		keyShardedRepo:   keyShardedRepo,
		robinShardedRepo: robinShardedRepo,
		domainName:       domainName,
	}
}

type URLCreatedResponse struct {
	Link string `json:"url"`
}

func (ctrl *URLShortner) BuildResponse(u *models.URL) *URLCreatedResponse {
	uri, err := url.Parse(ctrl.domainName)
	if err != nil {
		return &URLCreatedResponse{}
	}

	uri.Scheme = "https"
	uri.Path = u.ShortKey

	return &URLCreatedResponse{
		Link: uri.String(),
	}
}

type CreateURLReq struct {
	URL string `form:"url" json:"url" query:"url"`
}

func (ctrl *URLShortner) Post(c echo.Context) error {
	req := c.Request()
	ctx := req.Context()

	accept := req.Header.Get("Accept")
	expectsJSONResp := strings.EqualFold(accept, AcceptTypeJSON)

	body := &CreateURLReq{}

	if err := c.Bind(body); err != nil {
		if expectsJSONResp {
			return c.JSON(http.StatusBadRequest, `{"success": false, "error": "expected url"}`)
		}

		return c.HTML(http.StatusBadRequest, `<html><body>Missing URL</body></html>`)
	}

	checker := config.NewURLChecker(config.DefaultOptions())
	issues, err := checker.ValidateURL(body.URL)
	fmt.Println("kkkkkkk", err)

	if err != nil {
		log.Error().Err(err).Msg("invalid url")

		if expectsJSONResp {
			return c.JSON(http.StatusBadRequest, `{"success": false, "error": "url seems suspicious"}`)
		}

		return c.HTML(http.StatusBadRequest, `<html><body>URL is too malicious</body></html>`)

	}

	log.Info().Msgf("issues %v", issues)

	if len(issues) > config.CutoffMaxIssues {
		if expectsJSONResp {
			return c.JSON(http.StatusBadRequest, `{"success": false, "error": "url seems suspicious"}`)
		}

		return c.HTML(http.StatusBadRequest, `<html><body>URL is too malicious</body></html>`)
	}

	u, err := ctrl.robinShardedRepo.AssignURL(ctx, body.URL)
	if err != nil {
		if expectsJSONResp {
			return c.JSON(http.StatusInternalServerError, `{"success": false, "error": "something went wrong"}`)
		}

		return c.HTML(http.StatusInternalServerError, `<html><body>Something went wrong</body></html>`)
	}

	resp := ctrl.BuildResponse(u)

	if expectsJSONResp {
		return c.JSON(http.StatusCreated, resp)
	}

	return c.HTML(http.StatusCreated, fmt.Sprintf(`<html><body>%s</html></body>`, resp.Link))
}

func (ctrl *URLShortner) Get(c echo.Context) error {
	req := c.Request()
	accept := req.Header.Get("Accept")
	shortKey := strings.TrimSpace(c.Param("shortKey"))

	log.Info().Str("shortKey", shortKey).Msg("fetching url")

	expectsJSONResp := strings.EqualFold(accept, AcceptTypeJSON)

	if shortKey == "" || len(shortKey) > 12 {
		errCode := http.StatusBadRequest

		if expectsJSONResp {
			return c.JSON(errCode, `{"success": false, "error": "empty_url"}`)
		}

		return c.HTML(errCode, `<html><body>Fuck off</body></html>`)
	}

	u, err := ctrl.keyShardedRepo.Find(req.Context(), shortKey)
	if u != nil && u.Link == nil {
		err = errors.New("unassigned")
	}

	if err != nil {
		log.Error().Err(err).Msgf("failed to get url from short key %s", shortKey)

		if expectsJSONResp {
			return c.JSON(http.StatusNotFound, `{"success": false, "error": "not_found"}`)
		}

		return c.HTML(http.StatusNotFound, `<html><body>Not Found</body></html>`)
	}

	link, err := url.QueryUnescape(*u.Link)
	if err != nil {
		link = *u.Link
	}

	if expectsJSONResp {
		return c.JSON(http.StatusOK, fmt.Sprintf(`{"success": true, "url": "%s"}`, link))
	}

	return c.Redirect(http.StatusTemporaryRedirect, link)
}
