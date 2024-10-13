package do

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"log"
	"net/http"
	"strconv"
	"yvpn_server/auth"
	"yvpn_server/db"
)

func HandleDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		log.Println(err)
		http.Error(w, "invalid endpoint id", http.StatusBadRequest)
		return
	}

	e, err := db.GetEndpoint(endpointID)
	if err != nil {
		log.Println(err)
		http.Error(w, "retrieving endpoint record", http.StatusBadRequest)
		return
	}

	// delete the endpoint's client configs
	e.DeleteClientConfigsForEndpoint()

	// delete the endpoint on DO
	accountID := r.Context().Value("id").(uuid.UUID)
	a, err := auth.GetAccount(accountID)
	if err != nil || accountID != e.AccountID {
		log.Println(err)
		http.Error(w, "error associating account with endpoint", http.StatusBadRequest)
		return
	}

	err = DeleteEndpoint(endpointID, a.DigitalOceanToken)
	if err != nil {
		log.Println(err)
		http.Error(w, "deleting endpoint in DO", http.StatusBadRequest)
		return
	}

	// delete from the database
	err = e.Delete()
	if err != nil {
		log.Println(err)
		http.Error(w, "deleting endpoint", http.StatusBadRequest)
		return
	}

	// return updated html
	w.Header().Set("HX-Redirect", "/dashboard")
}

func HandleCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Println(err)
		http.Error(w, "parsing form", http.StatusBadRequest)
		return
	}

	a, err := auth.GetAccount(r.Context().Value("id").(uuid.UUID))
	if err != nil {
		log.Println(err)
		http.Error(w, "no account", http.StatusUnauthorized)
		return
	}

	endpoint := NewEndpoint{
		Token:      a.DigitalOceanToken,
		AccountID:  a.ID,
		Datacenter: r.FormValue("datacenter"),
	}

	err = endpoint.Create()
	if err != nil {
		log.Println(err)
		http.Error(w, "saving token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/dashboard")
}
