package testgen

import "time"

func at(y int, mo time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, mo, d, h, mi, s, 0, time.UTC)
}

// Places used by scenarios. They pick the timezone, so each is far from a border.
var (
	moscow = [2]float64{55.7558, 37.6173}   // Europe/Moscow, UTC+3
	la     = [2]float64{34.0522, -118.2437} // America/Los_Angeles
	tokyo  = [2]float64{35.6762, 139.6503}  // Asia/Tokyo, UTC+9
)

func geo(p [2]float64) (float64, float64) { return p[0], p[1] }

// Corpus is the synthetic export every OS runs in CI. It follows Google's
// layout: year folders, album folders, localized names, two zip parts, and
// sidecars in several of the naming styles Google has used.
func Corpus() *Takeout {
	t := New()
	y19 := "Photos from 2019"
	seed := 0
	next := func() int { seed++; return seed }

	// Stills with sidecars.
	t.Row("still-gps-sets-local-time-and-offset")
	lat, lon := geo(moscow)
	gpsBytes := JPEG(next())
	t.Photo(1, y19, "gps-moscow.jpg", gpsBytes, &Side{Taken: at(2019, 6, 6, 11, 23, 31), Lat: lat, Lon: lon})

	t.Row("still-geo-zero-means-no-location")
	t.Photo(1, y19, "zero-geo.jpg", JPEG(next()), &Side{Taken: at(2019, 6, 7, 9, 0, 0)})

	t.Row("still-caption-with-line-break")
	captionBytes := JPEG(next())
	t.Photo(1, y19, "caption.jpg", captionBytes, &Side{Taken: at(2019, 6, 8, 9, 0, 0), Description: "first line\n-execute\nsecond line"})

	t.Row("still-camera-date-within-2s-is-kept")
	t.Photo(1, y19, "exif-close.jpg", JPEGWithDate(next(), "2019:06:09 09:00:01"), &Side{Taken: at(2019, 6, 9, 9, 0, 0)})

	t.Row("still-camera-date-far-off-is-replaced")
	t.Photo(1, y19, "exif-far.jpg", JPEGWithDate(next(), "2001:01:01 00:00:00"), &Side{Taken: at(2019, 6, 10, 9, 0, 0)})

	t.Row("still-png")
	t.Photo(1, y19, "real.png", PNG(next()), &Side{Taken: at(2019, 6, 11, 9, 0, 0)})

	t.Row("still-webp")
	t.Photo(1, y19, "still.webp", Fixture("still1.webp"), &Side{Taken: at(2019, 6, 12, 9, 0, 0)})

	t.Row("still-heic")
	lat, lon = geo(tokyo)
	t.Photo(1, y19, "IMG_0100.HEIC", Unique(Fixture("still1.heic"), "heic-a"), &Side{Taken: at(2019, 6, 13, 1, 0, 0), Lat: lat, Lon: lon})

	t.Row("still-jpg-name-png-bytes-gets-png-extension")
	t.Photo(1, y19, "actually-png.jpg", PNG(next()), &Side{Taken: at(2019, 6, 14, 9, 0, 0)})

	t.Row("gpth-395-photoTakenTime-missing-uses-creationTime")
	t.Photo(1, y19, "created-only.jpg", JPEG(next()), &Side{Created: at(2019, 6, 15, 9, 0, 0)})

	t.Row("gpth-377-geoDataExif-missing")
	lat, lon = geo(moscow)
	t.Photo(1, y19, "no-geo-exif.jpg", JPEG(next()), &Side{Taken: at(2019, 6, 16, 9, 0, 0), Lat: lat, Lon: lon, NoGeoExif: true})

	t.Row("gpth-14-camera-hour-24-does-not-crash")
	t.Photo(1, y19, "hour-24.jpg", JPEGWithDate(next(), "2016:01:26 24:48:30"), &Side{Taken: at(2016, 1, 25, 23, 48, 29)})

	// No sidecar: the date comes from the name, or the file goes to unknown/.
	t.Row("no-sidecar-date-from-camera-name")
	t.Put(1, y19, "IMG_20190606_142331.jpg", JPEG(next()))
	t.Row("no-sidecar-date-from-screenshot-name")
	t.Put(1, y19, "Screenshot_20190607-101010.png", PNG(next()))
	t.Row("gpth-395-whatsapp-name-date-only")
	t.Put(1, y19, "IMG-20190203-WA0026.jpg", JPEG(next()))
	t.Row("gpth-436-date-prefix-and-uuid")
	t.Put(1, y19, "2019-04-21_640fea6c-bb0a-cf02-951c-00d09ac2d3cc.jpg", JPEG(next()))
	t.Row("gpth-32-scan-dated-1965-keeps-its-date")
	t.Photo(1, "Photos from 1965", "scan-1965.jpg", JPEG(next()), &Side{Taken: at(1965, 6, 1, 12, 0, 0), Created: at(2019, 3, 1, 9, 0, 0)})
	t.Row("gpth-436-epoch-zero-taken-uses-creationTime")
	t.Photo(1, y19, "epoch.jpg", JPEG(next()), &Side{Taken: time.Unix(0, 0).UTC(), Created: at(2019, 3, 2, 9, 0, 0)})
	t.Row("no-sidecar-no-date-goes-to-unknown")
	t.Put(1, y19, "no-date-at-all.jpg", JPEG(next()))

	// Sidecar naming styles.
	t.Row("sidecar-plain-json")
	t.Put(1, y19, "short.jpg", JPEG(next()))
	t.SideAs(1, y19, "short.jpg.json", "short.jpg", Side{Taken: at(2019, 7, 1, 9, 0, 0)})

	t.Row("gpth-353-sidecar-truncated-to-51-bytes")
	long := "a_very_long_file_name_for_truncation_tests_x.jpg" // 48 bytes
	t.Put(1, y19, long, JPEG(next()))
	t.SideAs(1, y19, fit51(long+".supplemental-metadata"), long, Side{Taken: at(2019, 7, 2, 9, 0, 0)})

	t.Row("gpth-448-sidecar-suppl-truncation-uuid-name")
	uuid := "0bca7b90-299e-4000-b29a-d97037b18456.jpg"
	t.Put(1, y19, uuid, JPEG(next()))
	t.SideAs(1, y19, fit51(uuid+".supplemental-metadata"), uuid, Side{Taken: at(2019, 7, 3, 9, 0, 0)})

	t.Row("sidecar-bracket-number-moves-before-json")
	t.Put(1, y19, "dup(1).jpg", JPEG(next()))
	t.SideAs(1, y19, "dup.jpg.supplemental-metadata(1).json", "dup.jpg", Side{Taken: at(2019, 7, 4, 9, 0, 0)})

	t.Row("sidecar-edited-copy-shares-original-sidecar")
	t.Photo(1, y19, "edited.jpg", JPEG(next()), &Side{Taken: at(2019, 7, 5, 9, 0, 0)})
	t.Put(1, y19, "edited-edited.jpg", JPEG(next()))

	t.Row("sidecar-in-another-zip-part")
	t.Put(1, y19, "split.jpg", JPEG(next()))
	t.SideAs(2, y19, "split.jpg.supplemental-metadata.json", "split.jpg", Side{Taken: at(2019, 7, 6, 9, 0, 0)})

	t.Row("gpth-46-sidecar-name-case-differs")
	t.Put(1, y19, "IMG_CASE.JPG", JPEG(next()))
	t.SideAs(1, y19, "IMG_CASE.jpg.supplemental-metadata.json", "IMG_CASE.JPG", Side{Taken: at(2019, 7, 12, 9, 0, 0)})

	t.Row("gpth-46-two-case-variant-sidecars-match-neither")
	t.Put(1, y19, "AMB.jpg", JPEG(next()))
	t.SideAs(1, y19, "amb.JPG.json", "amb.JPG", Side{Taken: at(2019, 7, 13, 9, 0, 0)})
	t.SideAs(1, y19, "Amb.Jpg.json", "Amb.Jpg", Side{Taken: at(2019, 7, 14, 9, 0, 0)})

	t.Row("gpth-460-metadata-json-variant")
	t.Put(1, y19, "meta.jpg", JPEG(next()))
	t.SideAs(1, y19, "meta.jpg.metadata.json", "meta.jpg", Side{Taken: at(2019, 7, 15, 9, 0, 0)})

	t.Row("sidecar-same-name-other-folder-does-not-match")
	t.Put(1, y19, "lonely.jpg", JPEG(next()))
	t.SideAs(1, "Photos from 2018", "lonely.jpg.supplemental-metadata.json", "lonely.jpg", Side{Taken: at(2018, 7, 7, 9, 0, 0)})

	t.Row("name-nfd-unicode")
	t.Photo(1, y19, "café.jpg", JPEG(next()), &Side{Taken: at(2019, 7, 8, 9, 0, 0)})
	t.Row("name-cyrillic-and-emoji")
	t.Photo(1, y19, "Ёлка 🌅.jpg", JPEG(next()), &Side{Taken: at(2019, 7, 9, 9, 0, 0)})
	t.Row("name-windows-reserved-and-illegal")
	t.Photo(1, y19, "CON.jpg", JPEG(next()), &Side{Taken: at(2019, 7, 10, 9, 0, 0)})
	t.Photo(1, y19, "what?.jpg", JPEG(next()), &Side{Taken: at(2019, 7, 11, 9, 0, 0)})
	t.Photo(1, y19, "what*.jpg", JPEG(next()), &Side{Taken: at(2019, 7, 11, 9, 0, 1)})

	// Videos.
	t.Row("video-mp4-gps")
	lat, lon = geo(la)
	t.Photo(1, y19, "trip-video.mp4", Unique(Fixture("clip1.mp4"), "mp4-a"), &Side{Taken: at(2019, 8, 1, 20, 0, 0), Lat: lat, Lon: lon})
	t.Row("video-mov")
	t.Photo(1, y19, "plain.mov", Unique(Fixture("clip1.mov"), "mov-a"), &Side{Taken: at(2019, 8, 2, 9, 0, 0)})
	t.Row("video-3gp")
	t.Photo(1, y19, "phone.3gp", Unique(Fixture("clip1.3gp"), "3gp-a"), &Side{Taken: at(2019, 8, 3, 9, 0, 0)})
	t.Row("video-mkv-converted-to-mov")
	t.Photo(1, y19, "old.mkv", Fixture("clip1.mkv"), &Side{Taken: at(2019, 8, 4, 9, 0, 0)})
	t.Row("video-webm-converted-to-mov")
	t.Photo(1, y19, "clip.webm", Fixture("clip1.webm"), &Side{Taken: at(2019, 8, 5, 9, 0, 0)})

	// Live Photos.
	t.Row("live-heic-mov-pair")
	lat, lon = geo(tokyo)
	t.Photo(1, y19, "IMG_0001.HEIC", Unique(Fixture("still2.heic"), "live-a"), &Side{Taken: at(2019, 9, 1, 1, 0, 0), Lat: lat, Lon: lon})
	t.Photo(1, y19, "IMG_0001.MOV", Unique(Fixture("clip2.mov"), "live-a"), &Side{Taken: at(2019, 9, 1, 1, 0, 0), Lat: lat, Lon: lon})
	t.Row("live-jpeg-mp4-pair-across-zips")
	t.Photo(1, y19, "PXL_0002.jpg", JPEG(next()), &Side{Taken: at(2019, 9, 2, 9, 0, 0)})
	t.Photo(2, y19, "PXL_0002.mp4", Unique(Fixture("clip2.mp4"), "live-b"), &Side{Taken: at(2019, 9, 2, 9, 0, 0)})

	// Camera RAW.
	t.Row("gpth-462-dng-raw-tagged")
	lat, lon = geo(la)
	t.Photo(1, y19, "RAW_0001.dng", TIFFRAW(next()), &Side{Taken: at(2019, 9, 20, 9, 0, 0), Lat: lat, Lon: lon})
	t.Row("gpth-271-nef-raw-tagged")
	t.Photo(1, y19, "DSC_0002.NEF", TIFFRAW(next()), &Side{Taken: at(2019, 9, 21, 9, 0, 0)})
	t.Row("raw-cr3-and-jpeg-same-name-are-not-a-live-photo")
	t.Photo(1, y19, "IMG_0003.CR3", CR3(next()), &Side{Taken: at(2019, 9, 22, 9, 0, 0)})
	t.Photo(1, y19, "IMG_0003.JPG", JPEG(next()), &Side{Taken: at(2019, 9, 22, 9, 0, 0)})
	t.Row("tiff-scan-tagged")
	t.Photo(1, y19, "scan.tif", TIFFRAW(next()), &Side{Taken: at(2019, 9, 23, 9, 0, 0)})

	t.Row("gpth-460-live-video-without-sidecar-takes-still-date")
	lat, lon = geo(tokyo)
	t.Photo(1, y19, "IMG_0010.HEIC", Unique(Fixture("still2.heic"), "live-c"), &Side{Taken: at(2019, 9, 10, 1, 0, 0), Lat: lat, Lon: lon})
	t.Put(1, y19, "IMG_0010.MOV", Unique(Fixture("clip2.mov"), "live-c"))
	t.Row("gpth-350-live-halves-share-a-sidecar-titled-as-still")
	t.Put(1, y19, "IMG_0012.HEIC", Unique(Fixture("still2.heic"), "live-d"))
	t.Put(1, y19, "IMG_0012.MOV", Unique(Fixture("clip2.mov"), "live-d"))
	t.SideAs(1, y19, "IMG_0012.json", "IMG_0012.HEIC", Side{Taken: at(2019, 9, 12, 9, 0, 0)})
	t.Row("gpth-180-pixel-mp-video-pairs-with-mp-jpg")
	lat, lon = geo(la)
	t.Photo(1, y19, "PXL_20190913_160000000.MP.jpg", JPEG(next()), &Side{Taken: at(2019, 9, 13, 16, 0, 0), Lat: lat, Lon: lon})
	t.Put(1, y19, "PXL_20190913_160000000.MP", Unique(Fixture("clip2.mp4"), "live-e"))
	t.Row("gpth-324-pixel-mp-tilde-copy-is-a-plain-video")
	t.Put(1, y19, "PXL_20190914_160000000.MP.jpg", JPEG(next()))
	t.Put(1, y19, "PXL_20190914_160000000.MP", Unique(Fixture("clip2.mp4"), "live-f"))
	t.Put(1, y19, "PXL_20190914_160000000.MP~2", Unique(Fixture("clip2.mp4"), "live-g"))

	t.Row("avi-and-bmp-placed-with-file-dates-only")
	t.Photo(1, y19, "old-camera.avi", AVI(next()), &Side{Taken: at(2019, 9, 24, 9, 0, 0)})
	t.Photo(1, y19, "scan.bmp", BMP(next()), &Side{Taken: at(2019, 9, 25, 9, 0, 0)})

	t.Row("gif-gets-xmp-date-only")
	t.Photo(1, y19, "anim.gif", GIF(next()), &Side{Taken: at(2019, 10, 1, 9, 0, 0)})

	// Localized year folders and system folders.
	t.Row("gpth-461-year-folder-german")
	t.Photo(1, "Fotos von 2018", "de.jpg", JPEG(next()), &Side{Taken: at(2018, 5, 1, 9, 0, 0)})
	t.Row("year-folder-russian")
	t.Photo(1, "Фото за 2017", "ru.jpg", JPEG(next()), &Side{Taken: at(2017, 5, 1, 9, 0, 0)})
	t.Row("archive-folder-is-library")
	t.Photo(1, "Archive", "archived.jpg", JPEG(next()), &Side{Taken: at(2019, 11, 1, 9, 0, 0)})
	t.Row("trash-is-skipped-by-default")
	t.Photo(1, "Trash", "trashed.jpg", JPEG(next()), &Side{Taken: at(2019, 11, 2, 9, 0, 0)})

	// Albums.
	t.Row("album-duplicate-of-library-photo")
	t.Photo(2, `Trip: "A|B"?`, "gps-moscow.jpg", gpsBytes, &Side{Taken: at(2019, 6, 6, 11, 23, 31)})
	t.Photo(2, "Café & Friends 🎉", "caption.jpg", captionBytes, &Side{Taken: at(2019, 6, 8, 9, 0, 0)})
	t.Row("gpth-366-album-only-file-joins-library")
	t.Photo(2, `Trip: "A|B"?`, "album-only.jpg", JPEG(next()), &Side{Taken: at(2019, 12, 1, 9, 0, 0)})
	t.Row("album-names-differing-only-in-case")
	t.Photo(2, "Trip", "t1.jpg", JPEG(next()), &Side{Taken: at(2019, 12, 2, 9, 0, 0)})
	t.Photo(2, "trip", "t2.jpg", JPEG(next()), &Side{Taken: at(2019, 12, 3, 9, 0, 0)})
	t.Row("album-name-reserved-and-trailing-dot")
	t.Photo(2, "CON", "c1.jpg", JPEG(next()), &Side{Taken: at(2019, 12, 4, 9, 0, 0)})
	t.Photo(2, "trailing dot.", "d1.jpg", JPEG(next()), &Side{Taken: at(2019, 12, 5, 9, 0, 0)})
	t.Row("album-files-colliding-after-sanitizing")
	t.Photo(2, "Collide", "x?.jpg", JPEG(next()), &Side{Taken: at(2019, 12, 6, 9, 0, 0)})
	t.Photo(2, "Collide", "x*.jpg", JPEG(next()), &Side{Taken: at(2019, 12, 6, 9, 0, 1)})

	// Files an OS adds when a Takeout is unzipped and zipped again on a Mac or
	// opened in Windows Explorer.
	t.Row("gpth-203-appledouble-files-ignored")
	t.Put(1, y19, "._gps-moscow.jpg", []byte("\x00\x05\x16\x07AppleDouble"))
	t.PutRaw(1, "__MACOSX/Takeout/Google Photos/"+y19+"/._edited.jpg", []byte("\x00\x05\x16\x07"))
	t.Row("gpth-297-ds-store-and-thumbs-db-ignored")
	t.Put(1, y19, ".DS_Store", []byte("Bud1"))
	t.Put(1, y19, "Thumbs.db", []byte("thumbs"))
	t.Put(2, "Trip", "desktop.ini", []byte("[.ShellClassInfo]"))
	t.Row("symlink-entry-skipped")
	t.PutLink(1, y19, "link.jpg", "../../../etc/passwd")
	return t
}

// fit51 truncates a sidecar name so the whole file name, including ".json",
// is at most 51 bytes, as Google does.
func fit51(base string) string {
	full := base + ".json"
	if len(full) <= 51 {
		return full
	}
	return base[:51-len(".json")] + ".json"
}
