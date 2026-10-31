package macos

import (
	"encoding/json"
	"fmt"
	"time"
)

// readScript is the read script from docs/design/backend-macos.md. Fields are
// separated by character id 31 (U+001F).
const readScript = `
on txt(v)
	if v is missing value then return ""
	return v as string
end txt

if not (running of application id "com.spotify.client") then return "NOT_RUNNING"

tell application id "com.spotify.client"
	set pstate to (player state as string)
	if pstate is "stopped" then return "STOPPED"
	set sep to (character id 31)
	set t to missing value
	try
		set t to current track
	end try
	if t is missing value then return "NO_TRACK"

	set vName to ""
	try
		set vName to my txt(name of t)
	end try
	set vArtist to ""
	try
		set vArtist to my txt(artist of t)
	end try
	set vAlbum to ""
	try
		set vAlbum to my txt(album of t)
	end try
	set vAlbumArtist to ""
	try
		set vAlbumArtist to my txt(album artist of t)
	end try
	set vDuration to ""
	try
		set vDuration to my txt(duration of t)
	end try
	set vPosition to ""
	try
		set vPosition to my txt((player position * 1000) as integer)
	end try
	set vID to ""
	try
		set vID to my txt(id of t)
	end try
	set vSpotifyURL to ""
	try
		set vSpotifyURL to my txt(spotify url of t)
	end try
	set vArtwork to ""
	try
		set vArtwork to my txt(artwork url of t)
	end try
	set vTrackNumber to ""
	try
		set vTrackNumber to my txt(track number of t)
	end try
	set vDiscNumber to ""
	try
		set vDiscNumber to my txt(disc number of t)
	end try

	return "OK" & sep & pstate & sep & vName & sep & vArtist & sep & vAlbum & sep & vAlbumArtist & sep & vDuration & sep & vPosition & sep & vID & sep & vSpotifyURL & sep & vArtwork & sep & vTrackNumber & sep & vDiscNumber
end tell
`

// listenerTemplate is the change listener, a JavaScript for Automation
// program with two verbs: %[1]s for the quoted notification name and %[2]g for
// the heartbeat in seconds. It registers an observer for that distributed
// notification, prints "ready", then prints "changed" for each notification
// and "tick" every heartbeat. It sends no Apple events: it never launches
// Spotify and needs no Automation permission. The observer stays in a global
// because the notification center does not retain it.
//
// The tick makes the listener die with its reader: once spmeta is gone, even
// after SIGKILL, writing to stdout raises SIGPIPE, or fails when SIGPIPE is
// ignored and the script exits.
const listenerTemplate = `ObjC.import('Foundation');
ObjC.import('stdlib');

function say(line) {
	var data = $(line + '\n').dataUsingEncoding($.NSUTF8StringEncoding);
	if (!$.NSFileHandle.fileHandleWithStandardOutput.writeDataError(data, null)) {
		$.exit(0);
	}
}

ObjC.registerSubclass({
	name: 'SpmetaObserver',
	methods: {
		'onChange:': {
			types: ['void', ['id']],
			implementation: function (notification) { say('changed'); }
		},
		'onTick:': {
			types: ['void', ['id']],
			implementation: function (timer) { say('tick'); }
		}
	}
});

var observer = $.SpmetaObserver.alloc.init;
$.NSDistributedNotificationCenter.defaultCenter.addObserverSelectorNameObject(
	observer, 'onChange:', %[1]s, $());
$.NSTimer.scheduledTimerWithTimeIntervalTargetSelectorUserInfoRepeats(%[2]g, observer, 'onTick:', $(), true);
say('ready');
$.NSRunLoop.currentRunLoop.run;
`

// listenerScript returns the change listener observing the distributed
// notification name and ticking every heartbeat.
func listenerScript(name string, heartbeat time.Duration) string {
	quoted, _ := json.Marshal(name) // a string always marshals
	return fmt.Sprintf(listenerTemplate, quoted, heartbeat.Seconds())
}
