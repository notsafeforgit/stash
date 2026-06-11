package transcoder

import "github.com/stashapp/stash/pkg/ffmpeg"

type ScreenshotOptions struct {
	OutputPath string
	OutputType ScreenshotOutputType

	// Quality is the quality scale. See https://ffmpeg.org/ffmpeg.html#Main-options
	Quality int

	// Width is the width to scale the screenshot to. If 0, no scaling will be applied.
	Width int
	// Height is the height to scale the screenshot to. If 0, no scaling will be applied.
	// Not used if Width is set.
	Height int

	// Verbosity is the logging verbosity. Defaults to LogLevelError if not set.
	Verbosity ffmpeg.LogLevel

	UseSelectFilter bool

	// SlowSeek uses accurate seek by placing -ss after the input.
	SlowSeek bool

	// SetBT709ColorParameters overrides invalid frame color tags before scaling.
	SetBT709ColorParameters bool
}

func (o *ScreenshotOptions) setDefaults() {
	if o.Verbosity == "" {
		o.Verbosity = ffmpeg.LogLevelError
	}
}

type ScreenshotOutputType struct {
	codec            *ffmpeg.VideoCodec
	format           ffmpeg.Format
	strictCompliance *int
}

func (t ScreenshotOutputType) Args() []string {
	var ret []string
	if t.codec != nil {
		ret = append(ret, t.codec.Args()...)
	}
	if t.strictCompliance != nil {
		var args ffmpeg.Args
		ret = append(ret, args.Strict(*t.strictCompliance)...)
	}
	if t.format != "" {
		ret = append(ret, t.format.Args()...)
	}

	return ret
}

var (
	strictUnofficial = -2

	ScreenshotOutputTypeImage2 = ScreenshotOutputType{
		format:           ffmpeg.FormatImage2,
		strictCompliance: &strictUnofficial,
	}
	ScreenshotOutputTypeBMP = ScreenshotOutputType{
		codec:  &ffmpeg.VideoCodecBMP,
		format: ffmpeg.FormatImage2Pipe,
	}
)

func ScreenshotTime(input string, t float64, options ScreenshotOptions) ffmpeg.Args {
	options.setDefaults()

	var args ffmpeg.Args
	args = args.LogLevel(options.Verbosity)
	args = args.Overwrite()

	if !options.SlowSeek {
		args = args.Seek(t)
	}
	if options.SetBT709ColorParameters {
		args = args.SetBT709ColorParameters()
	}
	args = args.Input(input)
	if options.SlowSeek {
		args = args.Seek(t)
	}
	args = args.VideoFrames(1)

	if options.Quality > 0 {
		args = args.FixedQualityScaleVideo(options.Quality)
	}

	var vf ffmpeg.VideoFilter

	if options.SetBT709ColorParameters {
		vf = vf.SetBT709ColorParameters()
	}

	if options.Width > 0 {
		vf = vf.ScaleWidth(options.Width)
	} else if options.Height > 0 {
		vf = vf.ScaleHeight(options.Height)
	}

	args = args.VideoFilter(vf)

	args = args.AppendArgs(options.OutputType)
	args = args.Output(options.OutputPath)

	return args
}

// ScreenshotFrame uses the select filter to get a single frame from the video.
// It is very slow and should only be used for files with very small duration in secs / frame count.
func ScreenshotFrame(input string, frame int, options ScreenshotOptions) ffmpeg.Args {
	options.setDefaults()

	var args ffmpeg.Args
	args = args.LogLevel(options.Verbosity)
	args = args.Overwrite()

	if options.SetBT709ColorParameters {
		args = args.SetBT709ColorParameters()
	}
	args = args.Input(input)
	args = args.VideoFrames(1)

	args = args.VSync(ffmpeg.VSyncMethodPassthrough)

	var vf ffmpeg.VideoFilter

	if options.SetBT709ColorParameters {
		vf = vf.SetBT709ColorParameters()
	}

	// keep only frame number options.Frame)
	vf = vf.Select(frame)

	if options.Width > 0 {
		vf = vf.ScaleWidth(options.Width)
	}

	args = args.VideoFilter(vf)

	args = args.AppendArgs(options.OutputType)
	args = args.Output(options.OutputPath)

	return args
}
