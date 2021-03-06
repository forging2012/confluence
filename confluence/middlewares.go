package confluence

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

func withTorrentContext(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var ih metainfo.Hash
		err := ih.FromHexString(q.Get("ih"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// TODO: Create abstraction for not refcounting Torrents.
		var ref *refclose.Ref
		grace := torrentCloseGraceForRequest(r)
		if grace >= 0 {
			ref = torrentRefs.NewRef(ih)
		}
		tc := torrentClientForRequest(r)
		t, new := tc.AddTorrentInfoHash(ih)
		if grace >= 0 {
			ref.SetCloser(t.Drop)
			defer time.AfterFunc(grace, ref.Release)
		}
		if new {
			mi := cachedMetaInfo(ih)
			if mi != nil {
				t.AddTrackers(mi.UpvertedAnnounceList())
				t.SetInfoBytes(mi.InfoBytes)
			}
			go saveTorrentWhenGotInfo(t)
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), torrentContextKey, t)))
	})
}

func saveTorrentWhenGotInfo(t *torrent.Torrent) {
	select {
	case <-t.Closed():
	case <-t.GotInfo():
	}
	err := saveTorrentFile(t)
	if err != nil {
		log.Printf("error saving torrent file: %s", err)
	}
}

func cachedMetaInfo(infoHash metainfo.Hash) *metainfo.MetaInfo {
	p := fmt.Sprintf("torrents/%s.torrent", infoHash.HexString())
	mi, err := metainfo.LoadFromFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		log.Printf("error loading metainfo file %q: %s", p, err)
	}
	return mi
}
