package engine

// Which Device sent the Pong applied to each of this Engine's Pings, and that
// Device's id for it, kept in the store so that only its sender can still
// withdraw it or send it again after the Engine restarts. A row goes once the
// Pong is delivered or left, withdrawn, its Device leaves the set, or its Ping
// is no longer kept.

// loadApplied makes the table and reads it into a.applied.
func (a *across) loadApplied() error {
	db := a.s.db
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS applied_pongs (
		ping_id TEXT PRIMARY KEY,
		device TEXT NOT NULL,
		pong_id TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM applied_pongs WHERE ping_id NOT IN (SELECT id FROM pings)`); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT ping_id, device, pong_id FROM applied_pongs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var ap appliedPong
		if err := rows.Scan(&id, &ap.from, &ap.pongID); err != nil {
			return err
		}
		a.applied[id] = ap
	}
	return rows.Err()
}

// storeApplied writes whose Pong is on Ping id, as a.applied has it, to the
// store; nothing when it is no longer anyone's.
func (a *across) storeApplied(id string) {
	// The lock is held over the write, so a drop cannot land between the
	// read and the write and leave a row behind.
	a.mu.Lock()
	defer a.mu.Unlock()
	ap, ok := a.applied[id]
	if !ok {
		return
	}
	a.s.db.Exec(`INSERT INTO applied_pongs (ping_id, device, pong_id) VALUES (?,?,?)
		ON CONFLICT (ping_id) DO UPDATE SET device = excluded.device, pong_id = excluded.pong_id`, id, ap.from, ap.pongID)
}

// dropApplied forgets whose Pong is on Ping id: nothing more can be done
// with it from another Device.
func (a *across) dropApplied(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, had := a.applied[id]
	delete(a.applied, id)
	if had {
		a.s.db.Exec(`DELETE FROM applied_pongs WHERE ping_id = ?`, id)
	}
}

// sentPong is a Pong sent from here for another computer's Ping: that
// computer, and the id given to the Pong, which it asks for to withdraw it.
// Kept in the store so the Pong can still be withdrawn after this Engine
// restarts. A row goes once that computer reports the Pong delivered, left
// or taken back, no longer reports its Ping, or leaves the set.
type sentPong struct{ computer, pongID string }

// loadSent makes the table and reads it into a.sentIDs.
func (a *across) loadSent() error {
	db := a.s.db
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sent_pongs (
		ping_id TEXT PRIMARY KEY,
		computer TEXT NOT NULL,
		pong_id TEXT NOT NULL
	)`); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT ping_id, computer, pong_id FROM sent_pongs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var sp sentPong
		if err := rows.Scan(&id, &sp.computer, &sp.pongID); err != nil {
			return err
		}
		a.sentIDs[id] = sp
	}
	return rows.Err()
}

// keepSent records the Pong sent from here for Ping id.
func (a *across) keepSent(id string, sp sentPong) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sentIDs[id] = sp
	a.s.db.Exec(`INSERT INTO sent_pongs (ping_id, computer, pong_id) VALUES (?,?,?)
		ON CONFLICT (ping_id) DO UPDATE SET computer = excluded.computer, pong_id = excluded.pong_id`, id, sp.computer, sp.pongID)
}

// dropSent forgets the Pong sent from here for Ping id.
func (a *across) dropSent(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, had := a.sentIDs[id]
	delete(a.sentIDs, id)
	if had {
		a.s.db.Exec(`DELETE FROM sent_pongs WHERE ping_id = ?`, id)
	}
}
