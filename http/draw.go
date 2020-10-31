package wasabeehttp

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/wasabee-project/Wasabee-Server"
)

func drawUploadRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	if !contentTypeIs(req, jsonTypeShort) {
		err := fmt.Errorf("invalid request (needs to be application/json)")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", "new operation")
		http.Error(res, jsonError(err), http.StatusNotAcceptable)
		return
	}

	// defer req.Body.Close()
	jBlob, err := ioutil.ReadAll(req.Body)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	if string(jBlob) == "" {
		err := fmt.Errorf("empty JSON on operation upload")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", "new operation")
		http.Error(res, jsonStatusEmpty, http.StatusNotAcceptable)
		return
	}

	jRaw := json.RawMessage(jBlob)
	if err = wasabee.DrawInsert(jRaw, gid); err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// the IITC plugin wants the full /me data on draw POST
	var ad wasabee.AgentData
	if err = gid.GetAgentData(&ad); err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	data, _ := json.Marshal(ad)
	res.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(res, string(data))
}

func drawGetRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	vars := mux.Vars(req)
	id := vars["document"]

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	var o wasabee.Operation
	o.ID = wasabee.OperationID(id)

	// o.Populate determines all or assigned-only
	read, _ := o.ReadAccess(gid)
	if !read && !o.AssignedOnlyAccess(gid) {
		if o.ID.IsDeletedOp() {
			err := fmt.Errorf("requested deleted op")
			wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", o.ID)
			http.Error(res, jsonError(err), http.StatusGone)
			return
		}

		err := fmt.Errorf("forbidden")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", o.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	if err = o.Populate(gid); err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	lastModified, err := time.ParseInLocation("2006-01-02 15:04:05", o.Modified, time.UTC)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	res.Header().Set("Last-Modified", lastModified.Format(time.RFC1123))
	res.Header().Set("Cache-Control", "no-store")

	ims := req.Header.Get("If-Modified-Since")
	if ims != "" && ims != "null" { // yes, the string "null", seen in the wild
		modifiedSince, err := time.ParseInLocation(time.RFC1123, ims, time.UTC)
		if err != nil {
			wasabee.Log.Error(err)
			http.Error(res, jsonError(err), http.StatusInternalServerError)
			return
		}
		if !lastModified.After(modifiedSince) {
			res.Header().Set("Content-Type", "")
			http.Redirect(res, req, "", http.StatusNotModified)
			return
		}
	}

	s, err := json.Marshal(o)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, string(s))
}

func drawDeleteRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)

	// only the ID needs to be set for this
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	// op.Delete checks ownership, do we need this check? -- yes for good status codes
	if !op.ID.IsOwner(gid) {
		err = fmt.Errorf("forbidden: only the owner can delete an operation")
		wasabee.Log.Warnw(err.Error(), "resource", op.ID, "GID", gid)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	if err := op.Delete(gid); err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	wasabee.Log.Infow("deleted operation", "resource", op.ID, "GID", gid, "message", "deleted operation")
	fmt.Fprint(res, jsonStatusOK)
}

func drawUpdateRoute(res http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	if !contentTypeIs(req, jsonTypeShort) {
		err := fmt.Errorf("invalid request (needs to be application/json)")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusNotAcceptable)
		return
	}

	jBlob, err := ioutil.ReadAll(req.Body)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	if string(jBlob) == "" {
		err := fmt.Errorf("empty JSON on operation upload")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonStatusEmpty, http.StatusNotAcceptable)
		return
	}

	jRaw := json.RawMessage(jBlob)

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("forbidden: write access required to update an operation")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	uid, err := wasabee.DrawUpdate(wasabee.OperationID(op.ID), jRaw, gid)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawChownRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	vars := mux.Vars(req)
	to := vars["to"]

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.ID.IsOwner(gid) {
		err = fmt.Errorf("forbidden: only the owner can set operation ownership ")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	err = op.ID.Chown(gid, to)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, jsonStatusOK)
}

func drawStockRoute(res http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	id := vars["document"]

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}

	var o wasabee.Operation
	o.ID = wasabee.OperationID(id)
	if err = o.Populate(gid); err != nil {
		wasabee.Log.Error(err)
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}

	url := "https://intel.ingress.com/intel/?z=13&ll="

	type latlon struct {
		lat string
		lon string
	}

	var portals = make(map[wasabee.PortalID]latlon)

	for _, p := range o.OpPortals {
		var l latlon
		l.lat = p.Lat
		l.lon = p.Lon
		portals[p.ID] = l
	}

	count := 0
	var notfirst bool
	for _, l := range o.Links {
		x := portals[l.From]
		if notfirst {
			url += "_"
		} else {
			url += x.lat + "," + x.lon + "&pls="
			notfirst = true
		}
		url += x.lat + "," + x.lon + ","
		y := portals[l.To]
		url += y.lat + "," + y.lon
		count++
		if count > 59 {
			break
		}
	}
	http.Redirect(res, req, url, http.StatusFound)
}

func drawLinkAssignRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("forbidden: write access required to assign agents")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}
	link := wasabee.LinkID(vars["link"])
	agent := wasabee.GoogleID(req.FormValue("agent"))
	uid, err := op.AssignLink(link, agent)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	// wasabee.Log.Infow("assigned link", "GID", gid, "resource", op.ID, "link", link, "agent", agent, "message", "assigned link")
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawLinkDescRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("write access required to set link descriptions")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}
	link := wasabee.LinkID(vars["link"])
	desc := req.FormValue("desc")
	uid, err := op.LinkDescription(link, desc)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawLinkColorRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("forbidden: write access required to set link color")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}
	link := wasabee.LinkID(vars["link"])
	color := req.FormValue("color")
	uid, err := op.LinkColor(link, color)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawLinkSwapRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("forbidden: write access required to swap link order")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	link := wasabee.LinkID(vars["link"])
	uid, err := op.LinkSwap(link)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawLinkZoneRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("forbidden: write access required to set zone")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	link := wasabee.LinkID(vars["link"])
	zone := wasabee.ZoneFromString(req.FormValue("zone"))

	uid, err := link.SetZone(&op, zone)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawLinkCompleteRoute(res http.ResponseWriter, req *http.Request) {
	drawLinkCompRoute(res, req, true)
}

func drawLinkIncompleteRoute(res http.ResponseWriter, req *http.Request) {
	drawLinkCompRoute(res, req, false)
}

func drawLinkCompRoute(res http.ResponseWriter, req *http.Request, complete bool) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	// write access OR asignee
	link := wasabee.LinkID(vars["link"])
	if !op.WriteAccess(gid) && !op.ID.AssignedTo(link, gid) {
		err = fmt.Errorf("permission to mark link as complete denied")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	uid, err := op.LinkCompleted(link, complete)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawMarkerAssignRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("write access required to assign targets")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	marker := wasabee.MarkerID(vars["marker"])
	agent := wasabee.GoogleID(req.FormValue("agent"))
	if err = op.Populate(gid); err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	uid, err := op.AssignMarker(marker, agent)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	wasabee.Log.Infow("assigned marker", "GID", gid, "resource", op.ID, "marker", marker, "agent", agent, "message", "assigned marker")
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawMarkerCommentRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("write access required to set marker comments")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	marker := wasabee.MarkerID(vars["marker"])
	comment := req.FormValue("comment")
	uid, err := op.MarkerComment(marker, comment)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawMarkerZoneRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("write access required to set marker zone")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	marker := wasabee.MarkerID(vars["marker"])
	zone := wasabee.ZoneFromString(req.FormValue("zone"))
	uid, err := marker.SetZone(&op, zone)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawMarkerFetch(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	// o.Populate determines all or assigned-only
	read, _ := op.ReadAccess(gid)
	if !read && !op.AssignedOnlyAccess(gid) {
		if op.ID.IsDeletedOp() {
			err := fmt.Errorf("requested deleted op")
			wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
			http.Error(res, jsonError(err), http.StatusGone)
			return
		}

		err := fmt.Errorf("forbidden")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	// populate the whole op, slow, but ensures we only get things we have access to see
	if err = op.Populate(gid); err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	markerID := wasabee.MarkerID(vars["marker"])
	marker, err := op.GetMarker(markerID)
	if err != nil {
		wasabee.Log.Error(err)
		// not really a 404, but close enough, better than a 500 or 403
		http.Error(res, jsonError(err), http.StatusNotFound)
		return
	}
	j, err := json.Marshal(marker)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, string(j))
}

func drawLinkFetch(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	// o.Populate determines all or assigned-only
	read, _ := op.ReadAccess(gid)
	if !read && !op.AssignedOnlyAccess(gid) {
		if op.ID.IsDeletedOp() {
			err := fmt.Errorf("requested deleted op")
			wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
			http.Error(res, jsonError(err), http.StatusGone)
			return
		}

		err := fmt.Errorf("forbidden")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	// populate the whole op, slow, but ensures we only get things we have access to see
	if err = op.Populate(gid); err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	linkID := wasabee.LinkID(vars["link"])
	link, err := op.GetLink(linkID)
	if err != nil {
		wasabee.Log.Error(err)
		// not really a 404, but close enough, better than a 500 or 403
		http.Error(res, jsonError(err), http.StatusNotFound)
		return
	}
	j, err := json.Marshal(link)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, string(j))
}

func drawPortalCommentRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("write access required to set portal comments")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	portalID := wasabee.PortalID(vars["portal"])
	comment := req.FormValue("comment")
	uid, err := op.PortalComment(portalID, comment)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawPortalHardnessRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	// only the ID needs to be set for this
	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if op.WriteAccess(gid) {
		err = fmt.Errorf("write access required to set portal hardness")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}
	portalID := wasabee.PortalID(vars["portal"])
	hardness := req.FormValue("hardness")
	uid, err := op.PortalHardness(portalID, hardness)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawOrderRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("write access required to set operation order")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	order := req.FormValue("order")
	_, err = op.LinkOrder(order, gid)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	uid, err := op.MarkerOrder(order, gid)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawInfoRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.WriteAccess(gid) {
		err = fmt.Errorf("write access required to set operation info")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}
	info := req.FormValue("info")
	uid, err := op.SetInfo(info, gid)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawPortalKeysRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])
	portalID := wasabee.PortalID(vars["portal"])

	onhand, err := strconv.ParseInt(req.FormValue("count"), 10, 32)
	if err != nil { // user supplied non-numeric value
		onhand = 0
	}
	if onhand < 0 { // @Robely42 .... sigh
		onhand = 0
	}
	// cap out at 3k, even though 2600 is the one-user absolute limit
	// because Niantic will Niantic
	if onhand > 3000 {
		onhand = 3000
	}
	capsule := req.FormValue("capsule")

	uid, err := op.KeyOnHand(gid, portalID, int32(onhand), capsule)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawMarkerCompleteRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])
	markerID := wasabee.MarkerID(vars["marker"])
	uid, err := markerID.Complete(op, gid)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawMarkerIncompleteRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])
	markerID := wasabee.MarkerID(vars["marker"])
	uid, err := markerID.Incomplete(op, gid)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawMarkerRejectRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])
	markerID := wasabee.MarkerID(vars["marker"])
	uid, err := markerID.Reject(&op, gid)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawMarkerAcknowledgeRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])
	markerID := wasabee.MarkerID(vars["marker"])
	uid, err := markerID.Acknowledge(&op, gid)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	wasabee.Log.Infow("acknowledged marker", "GID", gid, "resource", op.ID, "marker", markerID, "message", "acknowledged marker")
	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawStatRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)
	_, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])
	s, err := op.ID.Stat()
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}
	data, _ := json.Marshal(s)

	fmt.Fprint(res, string(data))
}

func drawMyRouteRoute(res http.ResponseWriter, req *http.Request) {
	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	var a wasabee.Assignments
	err = gid.Assignments(op.ID, &a)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	lls := "https://maps.google.com/maps/dir/?api=1"
	stops := len(a.Links) - 1

	if stops < 1 {
		res.Header().Set("Content-Type", jsonType)
		fmt.Fprint(res, `{ "status": "no assignments" }`)
		return
	}

	for i, l := range a.Links {
		if i == 0 {
			lls = fmt.Sprintf("%s&origin=%s,%s&waypoints=", lls, a.Portals[l.From].Lat, a.Portals[l.From].Lon)
		}
		// Google only allows 9 waypoints
		// we could do something fancy and show every (n) link based on len(a.Links) / 8
		// this is good enough for now
		if i < 7 {
			lls = fmt.Sprintf("%s|%s,%s", lls, a.Portals[l.From].Lat, a.Portals[l.From].Lon)
		}
		if i == stops { // last one -- even if > 10 in list
			lls = fmt.Sprintf("%s&destination=%s,%s", lls, a.Portals[l.From].Lat, a.Portals[l.From].Lon)
		}
	}

	http.Redirect(res, req, lls, http.StatusFound)
}

func drawPermsAddRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.ID.IsOwner(gid) {
		err = fmt.Errorf("permission to edit permissions denied")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	teamID := wasabee.TeamID(req.FormValue("team"))
	role := req.FormValue("role") // AddPerm verifies this is good
	if teamID == "" || role == "" {
		err = fmt.Errorf("required value not set to add permission to op")
		wasabee.Log.Warn(err)
		http.Error(res, jsonError(err), http.StatusNotAcceptable)
		return
	}
	// Pass in "Zeta" and get a zone back... defaults to "All"
	zone := wasabee.ZoneFromString(req.FormValue("zone"))

	uid, err := op.AddPerm(gid, teamID, role, zone)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func drawPermsDeleteRoute(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", jsonType)

	gid, err := getAgentID(req)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(req)
	var op wasabee.Operation
	op.ID = wasabee.OperationID(vars["document"])

	if !op.ID.IsOwner(gid) {
		err = fmt.Errorf("permission to edit permissions denied")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusForbidden)
		return
	}

	teamID := wasabee.TeamID(req.FormValue("team"))
	role := wasabee.OpPermRole(req.FormValue("role"))
	zone := wasabee.ZoneFromString(req.FormValue("zone"))
	if teamID == "" || role == "" {
		err = fmt.Errorf("required value not set to remove permission from op")
		wasabee.Log.Warnw(err.Error(), "GID", gid, "role", role, "zone", zone, "teamID", teamID, "resource", op.ID)
		http.Error(res, jsonError(err), http.StatusNotAcceptable)
		return
	}

	uid, err := op.DelPerm(gid, teamID, role, zone)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, jsonError(err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(res, jsonOKUpdateID(uid))
}

func jsonOKUpdateID(uid string) string {
	return fmt.Sprintf("{\"status\":\"ok\", \"updateID\": \"%s\"}", uid)
}
