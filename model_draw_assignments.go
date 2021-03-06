package wasabee

import (
	"database/sql"
)

// Assignments is used to show assignments to users in various ways
type Assignments struct {
	Links   []Link
	Markers []Marker
	Portals map[PortalID]Portal
}

// Assignments builds an Assignments struct for a user for an op
func (gid GoogleID) Assignments(opID OperationID, assignments *Assignments) error {
	var tmpLink Link
	var tmpMarker Marker
	var tmpPortal Portal
	var description, comment sql.NullString

	rows, err := db.Query("SELECT ID, fromPortalID, toPortalID, description, throworder FROM link WHERE opID = ? AND gid = ? ORDER BY throworder", opID, gid)
	if err != nil {
		Log.Error(err)
		return err
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&tmpLink.ID, &tmpLink.From, &tmpLink.To, &description, &tmpLink.ThrowOrder)
		if err != nil {
			Log.Error(err)
			continue
		}
		if description.Valid {
			tmpLink.Desc = description.String
		} else {
			tmpLink.Desc = ""
		}
		tmpLink.AssignedTo = gid
		assignments.Links = append(assignments.Links, tmpLink)
	}

	rows2, err := db.Query("SELECT ID, PortalID, type, gid, comment, state FROM marker WHERE opID = ? AND gid = ?", opID, gid)
	if err != nil {
		Log.Error(err)
		return err
	}
	defer rows2.Close()
	for rows2.Next() {
		err := rows2.Scan(&tmpMarker.ID, &tmpMarker.PortalID, &tmpMarker.Type, &tmpMarker.AssignedTo, &comment, &tmpMarker.State)
		if err != nil {
			Log.Error(err)
			continue
		}
		if comment.Valid {
			tmpMarker.Comment = comment.String
		} else {
			tmpMarker.Comment = ""
		}
		assignments.Markers = append(assignments.Markers, tmpMarker)
	}

	assignments.Portals = make(map[PortalID]Portal)
	rows3, err := db.Query("SELECT p.ID, p.name, Y(p.loc) AS lat, X(p.loc) AS lon FROM portal=p JOIN marker=m ON m.PortalID=p.ID AND p.opID=m.opID WHERE p.opID = ? AND m.gid = ? ORDER BY name", opID, gid)
	if err != nil {
		Log.Error(err)
		return err
	}
	defer rows3.Close()
	for rows3.Next() {
		err := rows3.Scan(&tmpPortal.ID, &tmpPortal.Name, &tmpPortal.Lat, &tmpPortal.Lon)
		if err != nil {
			Log.Error(err)
			continue
		}
		assignments.Portals[tmpPortal.ID] = tmpPortal
	}

	rows4, err := db.Query("SELECT p.ID, p.name, Y(p.loc) AS lat, X(p.loc) AS lon FROM portal=p JOIN link=l ON l.fromPortalID=p.ID AND p.opID=l.opID WHERE p.opID = ? AND l.gid = ? ORDER BY name", opID, gid)
	if err != nil {
		Log.Error(err)
		return err
	}
	defer rows4.Close()
	for rows4.Next() {
		err := rows4.Scan(&tmpPortal.ID, &tmpPortal.Name, &tmpPortal.Lat, &tmpPortal.Lon)
		if err != nil {
			Log.Error(err)
			continue
		}
		assignments.Portals[tmpPortal.ID] = tmpPortal
	}

	rows5, err := db.Query("SELECT p.ID, p.name, Y(p.loc) AS lat, X(p.loc) AS lon FROM portal=p JOIN link=l ON l.toPortalID=p.ID AND p.opID=l.opID WHERE p.opID = ? AND l.gid = ? ORDER BY name", opID, gid)
	if err != nil {
		Log.Error(err)
		return err
	}
	defer rows5.Close()
	for rows5.Next() {
		err := rows5.Scan(&tmpPortal.ID, &tmpPortal.Name, &tmpPortal.Lat, &tmpPortal.Lon)
		if err != nil {
			Log.Error(err)
			continue
		}
		assignments.Portals[tmpPortal.ID] = tmpPortal
	}
	return nil
}
