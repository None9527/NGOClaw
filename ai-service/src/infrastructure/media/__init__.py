"""Media Understanding Handler

Provides audio transcription and image/video analysis capabilities.
Uses whisper for audio transcription and vision-capable LLMs for image/video understanding.
"""

import base64
import io
import logging
import subprocess
import tempfile
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)


class MediaHandler:
    """Handles media transcription and analysis for the AI service."""

    def __init__(self, llm_provider=None, whisper_model: str = "base"):
        """Initialize the media handler.

        Args:
            llm_provider: Vision-capable LLM provider for image/video analysis.
            whisper_model: Whisper model size (tiny/base/small/medium/large).
        """
        self._llm_provider = llm_provider
        self._whisper_model = whisper_model
        self._whisper = None

    def _get_whisper(self):
        """Lazy-load whisper model."""
        if self._whisper is None:
            try:
                import whisper
                logger.info(f"Loading whisper model: {self._whisper_model}")
                self._whisper = whisper.load_model(self._whisper_model)
            except ImportError:
                logger.warning(
                    "openai-whisper not installed. "
                    "Install with: pip install openai-whisper"
                )
                raise RuntimeError("whisper not available")
        return self._whisper

    async def transcribe_audio(
        self,
        audio_data: bytes,
        mime_type: str = "audio/ogg",
        language: Optional[str] = None,
    ) -> dict:
        """Transcribe audio data to text.

        Args:
            audio_data: Raw audio bytes.
            mime_type: MIME type of the audio (audio/ogg, audio/mp3, etc).
            language: Optional language hint (ISO 639-1 code).

        Returns:
            Dict with 'text', 'language', and 'duration' keys.
        """
        suffix = _mime_to_ext(mime_type)

        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as tmp:
            tmp.write(audio_data)
            tmp_path = tmp.name

        try:
            model = self._get_whisper()
            options = {}
            if language:
                options["language"] = language

            result = model.transcribe(tmp_path, **options)
            return {
                "text": result.get("text", "").strip(),
                "language": result.get("language", "unknown"),
                "duration": _get_audio_duration(tmp_path),
            }
        finally:
            Path(tmp_path).unlink(missing_ok=True)

    async def analyze_image(
        self,
        image_data: bytes,
        mime_type: str = "image/jpeg",
        prompt: str = "Describe this image in detail.",
        model: Optional[str] = None,
    ) -> dict:
        """Analyze an image using a vision-capable LLM.

        Args:
            image_data: Raw image bytes.
            mime_type: MIME type of the image.
            prompt: Analysis prompt.
            model: Optional model override.

        Returns:
            Dict with 'description' and 'model_used' keys.
        """
        if self._llm_provider is None:
            return {
                "description": "[Vision LLM provider not configured]",
                "model_used": "none",
            }

        b64_image = base64.b64encode(image_data).decode("utf-8")
        data_uri = f"data:{mime_type};base64,{b64_image}"

        try:
            # Construct multimodal request
            response = await self._llm_provider.generate_with_vision(
                prompt=prompt,
                image_url=data_uri,
                model=model,
            )
            return {
                "description": response.get("content", ""),
                "model_used": response.get("model", "unknown"),
            }
        except Exception as e:
            logger.error(f"Image analysis failed: {e}")
            return {
                "description": f"[Analysis failed: {e}]",
                "model_used": "error",
            }

    async def analyze_video(
        self,
        video_data: bytes,
        mime_type: str = "video/mp4",
        prompt: str = "Describe what happens in this video.",
        max_frames: int = 8,
        model: Optional[str] = None,
    ) -> dict:
        """Analyze a video by extracting key frames and using vision LLM.

        Args:
            video_data: Raw video bytes.
            mime_type: MIME type of the video.
            prompt: Analysis prompt.
            max_frames: Maximum number of frames to extract.
            model: Optional model override.

        Returns:
            Dict with 'description', 'model_used', and 'frames_analyzed' keys.
        """
        suffix = _mime_to_ext(mime_type)

        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as tmp:
            tmp.write(video_data)
            video_path = tmp.name

        try:
            frames = extract_video_frames(video_path, max_frames)
            if not frames:
                return {
                    "description": "[No frames could be extracted]",
                    "model_used": "none",
                    "frames_analyzed": 0,
                }

            # Analyze each frame
            descriptions = []
            model_used = "unknown"
            for i, frame_data in enumerate(frames):
                result = await self.analyze_image(
                    image_data=frame_data,
                    mime_type="image/jpeg",
                    prompt=f"Frame {i+1}/{len(frames)}: {prompt}",
                    model=model,
                )
                descriptions.append(f"Frame {i+1}: {result['description']}")
                model_used = result["model_used"]

            return {
                "description": "\n\n".join(descriptions),
                "model_used": model_used,
                "frames_analyzed": len(frames),
            }
        finally:
            Path(video_path).unlink(missing_ok=True)


def extract_video_frames(
    video_path: str, max_frames: int = 8
) -> list[bytes]:
    """Extract evenly-spaced frames from a video using ffmpeg.

    Args:
        video_path: Path to the video file.
        max_frames: Maximum number of frames to extract.

    Returns:
        List of JPEG-encoded frame bytes.
    """
    try:
        # Get video duration
        result = subprocess.run(
            [
                "ffprobe",
                "-v", "error",
                "-show_entries", "format=duration",
                "-of", "csv=p=0",
                video_path,
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )
        duration = float(result.stdout.strip())

        # Calculate frame interval
        interval = max(duration / (max_frames + 1), 0.5)

        frames = []
        for i in range(max_frames):
            timestamp = interval * (i + 1)
            if timestamp >= duration:
                break

            frame_result = subprocess.run(
                [
                    "ffmpeg",
                    "-ss", str(timestamp),
                    "-i", video_path,
                    "-vframes", "1",
                    "-f", "image2pipe",
                    "-vcodec", "mjpeg",
                    "-q:v", "5",
                    "-",
                ],
                capture_output=True,
                timeout=10,
            )

            if frame_result.returncode == 0 and frame_result.stdout:
                frames.append(frame_result.stdout)

        return frames

    except (subprocess.TimeoutExpired, FileNotFoundError, ValueError) as e:
        logger.error(f"Frame extraction failed: {e}")
        return []


def _mime_to_ext(mime_type: str) -> str:
    """Convert MIME type to file extension."""
    mapping = {
        "audio/ogg": ".ogg",
        "audio/mpeg": ".mp3",
        "audio/mp3": ".mp3",
        "audio/wav": ".wav",
        "audio/flac": ".flac",
        "audio/m4a": ".m4a",
        "video/mp4": ".mp4",
        "video/webm": ".webm",
        "video/avi": ".avi",
        "image/jpeg": ".jpg",
        "image/png": ".png",
        "image/webp": ".webp",
        "image/gif": ".gif",
    }
    return mapping.get(mime_type, ".bin")


def _get_audio_duration(audio_path: str) -> float:
    """Get audio duration in seconds using ffprobe."""
    try:
        result = subprocess.run(
            [
                "ffprobe",
                "-v", "error",
                "-show_entries", "format=duration",
                "-of", "csv=p=0",
                audio_path,
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )
        return float(result.stdout.strip())
    except (subprocess.TimeoutExpired, FileNotFoundError, ValueError):
        return 0.0
