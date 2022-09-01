package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/bbrodriges/practicum-shortener/internal/auth"
	"github.com/bbrodriges/practicum-shortener/internal/store"
	"github.com/bbrodriges/practicum-shortener/models"
)

func (i *Instance) ShortenHandler(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Cannot read request body"))
		return
	}

	u, err := url.Parse(string(b))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Cannot parse given string as URL"))
		return
	}

	shortURL, err := i.shorten(r.Context(), u)
	if err != nil && !errors.Is(err, store.ErrConflict) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	status := http.StatusCreated
	if errors.Is(err, store.ErrConflict) {
		status = http.StatusConflict
	}

	w.WriteHeader(status)
	_, _ = w.Write([]byte(shortURL))
}

func (i *Instance) ShortenAPIHandler(w http.ResponseWriter, r *http.Request) {
	var req models.ShortenRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad request body given"))
		return
	}

	u, err := url.Parse(req.URL)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Cannot parse given string as URL"))
		return
	}

	shortURL, err := i.shorten(r.Context(), u)
	if err != nil && !errors.Is(err, store.ErrConflict) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")

	status := http.StatusCreated
	if errors.Is(err, store.ErrConflict) {
		status = http.StatusConflict
	}

	w.WriteHeader(status)
	err = json.NewEncoder(w).Encode(models.ShortenResponse{
		Result: shortURL,
	})

	if err != nil {
		fmt.Printf("cannot write response: %s", err)
	}
}

func (i *Instance) ExpandHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad ID given"))
		return
	}

	target, err := i.store.Load(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if errors.Is(err, store.ErrDeleted) {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", target.String())
	w.WriteHeader(http.StatusTemporaryRedirect)
}

func (i *Instance) UserURLsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	uid := auth.UIDFromContext(ctx)
	if uid == nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	urls, err := i.store.LoadUsers(ctx, *uid)
	if errors.Is(err, store.ErrNotFound) || len(urls) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var resp []models.URLResponse
	for id, u := range urls {
		resp = append(resp, models.URLResponse{
			ShortURL:    i.baseURL + "/" + id,
			OriginalURL: u.String(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (i *Instance) BatchShortenAPIHandler(w http.ResponseWriter, r *http.Request) {
	var req []models.BatchShortenRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad request body given"))
		return
	}

	if len(req) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Empty URLs list given"))
		return
	}

	var urls []*url.URL
	for _, pair := range req {
		u, err := url.Parse(pair.OriginalURL)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			msg := fmt.Sprintf("Cannot parse given string as URL: %s", pair.OriginalURL)
			_, _ = w.Write([]byte(msg))
			return
		}
		urls = append(urls, u)
	}

	shortURLs, err := i.shortenBatch(r.Context(), urls)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if len(shortURLs) != len(req) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("invalid shorten URLs length"))
		return
	}

	res := make([]models.BatchShortenResponse, 0, len(shortURLs))
	for i, shortURL := range shortURLs {
		res = append(res, models.BatchShortenResponse{
			CorrelationID: req[i].CorrelationID,
			ShortURL:      shortURL,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		fmt.Printf("cannot write response: %s", err)
	}
}

func (i *Instance) BatchRemoveAPIHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := auth.UIDFromContext(ctx)
	if uid == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var ids []string
	err := json.NewDecoder(r.Body).Decode(&ids)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad request body given"))
		return
	}

	if len(ids) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Empty IDs list given"))
		return
	}

	err = i.store.DeleteUsers(ctx, *uid, ids...)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (i *Instance) PingHandler(w http.ResponseWriter, r *http.Request) {
	if err := i.store.Ping(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (i *Instance) shorten(ctx context.Context, rawURL *url.URL) (shortURL string, err error) {
	uid := auth.UIDFromContext(ctx)

	var id string
	if uid != nil {
		id, err = i.store.SaveUser(ctx, *uid, rawURL)
	} else {
		id, err = i.store.Save(ctx, rawURL)
	}

	if err != nil && !errors.Is(err, store.ErrConflict) {
		return "", fmt.Errorf("cannot save URL to storage: %w", err)
	}
	return i.baseURL + "/" + id, err
}

func (i *Instance) shortenBatch(ctx context.Context, rawURLs []*url.URL) (shortURLs []string, err error) {
	uid := auth.UIDFromContext(ctx)

	var ids []string
	if uid != nil {
		ids, err = i.store.SaveUserBatch(ctx, *uid, rawURLs)
	} else {
		ids, err = i.store.SaveBatch(ctx, rawURLs)
	}

	if err != nil {
		return nil, fmt.Errorf("cannot save URL to storage: %w", err)
	}

	for _, id := range ids {
		shortURLs = append(shortURLs, fmt.Sprintf("%s/%s", i.baseURL, id))
	}

	return shortURLs, nil
}
