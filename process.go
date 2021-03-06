package main

/*
#cgo pkg-config: vips
#cgo LDFLAGS: -s -w
#include "vips.h"
*/
import "C"

import (
	"errors"
	"log"
	"os"
	"runtime"
	"unsafe"
)

type imageType int

const (
	UNKNOWN = C.UNKNOWN
	JPEG    = C.JPEG
	PNG     = C.PNG
	WEBP    = C.WEBP
	GIF     = C.GIF
)

var imageTypes = map[string]imageType{
	"jpeg": JPEG,
	"jpg":  JPEG,
	"png":  PNG,
	"webp": WEBP,
	"gif":  GIF,
}

type gravityType int

const (
	CENTER gravityType = iota
	NORTH
	EAST
	SOUTH
	WEST
	SMART
)

var gravityTypes = map[string]gravityType{
	"ce": CENTER,
	"no": NORTH,
	"ea": EAST,
	"so": SOUTH,
	"we": WEST,
	"sm": SMART,
}

type resizeType int

const (
	FIT resizeType = iota
	FILL
	CROP
)

var resizeTypes = map[string]resizeType{
	"fit":  FIT,
	"fill": FILL,
	"crop": CROP,
}

type processingOptions struct {
	format  imageType
}

var vipsSupportSmartcrop bool
var vipsTypeSupportLoad = make(map[imageType]bool)
var vipsTypeSupportSave = make(map[imageType]bool)

func initVips() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := C.vips_initialize(); err != 0 {
		C.vips_shutdown()
		log.Fatalln("unable to start vips!")
	}

	C.vips_cache_set_max_mem(100 * 1024 * 1024) // 100Mb
	C.vips_cache_set_max(500)

	if len(os.Getenv("IMGPROXY_VIPS_LEAK_CHECK")) > 0 {
		C.vips_leak_set(C.gboolean(1))
	}

	if len(os.Getenv("IMGPROXY_VIPS_CACHE_TRACE")) > 0 {
		C.vips_cache_set_trace(C.gboolean(1))
	}

	vipsSupportSmartcrop = C.vips_support_smartcrop() == 1

	if int(C.vips_type_find_load_go(C.JPEG)) != 0 {
		vipsTypeSupportLoad[JPEG] = true
	}
	if int(C.vips_type_find_load_go(C.PNG)) != 0 {
		vipsTypeSupportLoad[PNG] = true
	}
	if int(C.vips_type_find_load_go(C.WEBP)) != 0 {
		vipsTypeSupportLoad[WEBP] = true
	}
	if int(C.vips_type_find_load_go(C.GIF)) != 0 {
		vipsTypeSupportLoad[GIF] = true
	}

	if int(C.vips_type_find_save_go(C.JPEG)) != 0 {
		vipsTypeSupportSave[JPEG] = true
	}
	if int(C.vips_type_find_save_go(C.PNG)) != 0 {
		vipsTypeSupportSave[PNG] = true
	}
	if int(C.vips_type_find_save_go(C.WEBP)) != 0 {
		vipsTypeSupportSave[WEBP] = true
	}
}

func shutdownVips() {
	C.vips_shutdown()
}

func round(f float64) int {
	return int(f + .5)
}

func extractMeta(img *C.VipsImage) (int, int, int, bool) {
	width := int(img.Xsize)
	height := int(img.Ysize)

	angle := C.VIPS_ANGLE_D0
	flip := false

	orientation := C.vips_get_exif_orientation(img)
	if orientation >= 5 && orientation <= 8 {
		width, height = height, width
	}
	if orientation == 3 || orientation == 4 {
		angle = C.VIPS_ANGLE_D180
	}
	if orientation == 5 || orientation == 6 {
		angle = C.VIPS_ANGLE_D90
	}
	if orientation == 7 || orientation == 8 {
		angle = C.VIPS_ANGLE_D270
	}
	if orientation == 2 || orientation == 4 || orientation == 5 || orientation == 7 {
		flip = true
	}

	return width, height, angle, flip
}

func calcShink(scale float64, imgtype imageType) int {
	shrink := int(1.0 / scale)

	if imgtype != JPEG {
		return shrink
	}

	switch {
	case shrink >= 16:
		return 8
	case shrink >= 8:
		return 4
	case shrink >= 4:
		return 2
	}

	return 1
}

func processImage(data []byte, imgtype imageType, po processingOptions, t *timer) ([]byte, error) {
	defer C.vips_cleanup()
	defer keepAlive(data)

	img, err := vipsLoadImage(data, imgtype, 1)
	if err != nil {
		return nil, err
	}
	defer C.clear_image(&img)

	t.Check()

	return vipsSaveImage(img, po.format)
}

func vipsLoadImage(data []byte, imgtype imageType, shrink int) (*C.struct__VipsImage, error) {
	var img *C.struct__VipsImage
	if C.vips_load_buffer(unsafe.Pointer(&data[0]), C.size_t(len(data)), C.int(imgtype), C.int(shrink), &img) != 0 {
		return nil, vipsError()
	}
	return img, nil
}

func vipsSaveImage(img *C.struct__VipsImage, imgtype imageType) ([]byte, error) {
	var ptr unsafe.Pointer
	defer C.g_free_go(&ptr)

	err := C.int(0)

	imgsize := C.size_t(0)

	switch imgtype {
	case JPEG:
		err = C.vips_jpegsave_go(img, &ptr, &imgsize, 1, C.int(conf.Quality), 0)
	case PNG:
		err = C.vips_pngsave_go(img, &ptr, &imgsize)
	case WEBP:
		err = C.vips_webpsave_go(img, &ptr, &imgsize, 1, C.int(conf.Quality))
	}
	if err != 0 {
		return nil, vipsError()
	}

	return C.GoBytes(ptr, C.int(imgsize)), nil
}

func vipsImageHasAlpha(img *C.struct__VipsImage) bool {
	return C.vips_image_hasalpha_go(img) > 0
}

func vipsPremultiply(img **C.struct__VipsImage) (C.VipsBandFormat, error) {
	var tmp *C.struct__VipsImage

	format := C.vips_band_format(*img)

	if C.vips_premultiply_go(*img, &tmp) != 0 {
		return 0, vipsError()
	}

	C.swap_and_clear(img, tmp)
	return format, nil
}

func vipsUnpremultiply(img **C.struct__VipsImage, format C.VipsBandFormat) error {
	var tmp *C.struct__VipsImage

	if C.vips_unpremultiply_go(*img, &tmp) != 0 {
		return vipsError()
	}
	C.swap_and_clear(img, tmp)

	if C.vips_cast_go(*img, &tmp, format) != 0 {
		return vipsError()
	}
	C.swap_and_clear(img, tmp)

	return nil
}

func vipsResize(img **C.struct__VipsImage, scale float64) error {
	var tmp *C.struct__VipsImage

	if C.vips_resize_go(*img, &tmp, C.double(scale)) != 0 {
		return vipsError()
	}

	C.swap_and_clear(img, tmp)
	return nil
}

func vipsRotate(img **C.struct__VipsImage, angle int) error {
	var tmp *C.struct__VipsImage

	if C.vips_rot_go(*img, &tmp, C.VipsAngle(angle)) != 0 {
		return vipsError()
	}

	C.swap_and_clear(img, tmp)
	return nil
}

func vipsFlip(img **C.struct__VipsImage) error {
	var tmp *C.struct__VipsImage

	if C.vips_flip_horizontal_go(*img, &tmp) != 0 {
		return vipsError()
	}

	C.swap_and_clear(img, tmp)
	return nil
}

func vipsCrop(img **C.struct__VipsImage, left, top, width, height int) error {
	var tmp *C.struct__VipsImage

	if C.vips_extract_area_go(*img, &tmp, C.int(left), C.int(top), C.int(width), C.int(height)) != 0 {
		return vipsError()
	}

	C.swap_and_clear(img, tmp)
	return nil
}

func vipsSmartCrop(img **C.struct__VipsImage, width, height int) error {
	var tmp *C.struct__VipsImage

	if C.vips_smartcrop_go(*img, &tmp, C.int(width), C.int(height)) != 0 {
		return vipsError()
	}

	C.swap_and_clear(img, tmp)
	return nil
}

func vipsFixColourspace(img **C.struct__VipsImage) error {
	var tmp *C.struct__VipsImage

	if C.vips_image_guess_interpretation(*img) != C.VIPS_INTERPRETATION_sRGB {
		if C.vips_colourspace_go(*img, &tmp, C.VIPS_INTERPRETATION_sRGB) != 0 {
			return vipsError()
		}
		C.swap_and_clear(img, tmp)
	}

	return nil
}

func vipsImageCopyMemory(img **C.struct__VipsImage) error {
	var tmp *C.struct__VipsImage
	if tmp = C.vips_image_copy_memory(*img); tmp == nil {
		return vipsError()
	}
	C.swap_and_clear(img, tmp)
	return nil
}

func vipsError() error {
	return errors.New(C.GoString(C.vips_error_buffer()))
}
