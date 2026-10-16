import { useState, useRef } from 'react'
import { Play } from 'lucide-react'

interface ReactionVideosSectionProps {
  headline: string
  thumbnails: string[]
  videoUrl: string
}

export function ReactionVideosSection({ headline, thumbnails, videoUrl }: ReactionVideosSectionProps) {
  const [showVideo, setShowVideo] = useState(false)
  const videoRef = useRef<HTMLVideoElement>(null)

  return (
    <>
      <div className="space-y-3 mt-2">
        {headline && (
          <h2 className="text-center text-sm font-semibold text-gray-800">{headline}</h2>
        )}
        <div className="flex justify-center gap-2">
          {thumbnails.map((thumb, i) => (
            <button
              key={i}
              type="button"
              onClick={() => setShowVideo(true)}
              className="relative w-[30%] max-w-[140px] aspect-video rounded-xl overflow-hidden group cursor-pointer"
            >
              <img
                src={thumb}
                alt=""
                className="w-full h-full object-cover"
                loading="lazy"
              />
              <div className="absolute inset-0 bg-black/20 flex items-center justify-center group-hover:bg-black/30 transition-colors">
                <div className="w-8 h-8 rounded-full bg-white/90 flex items-center justify-center shadow-lg">
                  <Play className="h-4 w-4 text-gray-800 ml-0.5" fill="currentColor" />
                </div>
              </div>
            </button>
          ))}
        </div>
      </div>

      {showVideo && (
        <div
          className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center p-4"
          onClick={() => setShowVideo(false)}
        >
          <div
            className="relative w-full max-w-sm aspect-[9/16] rounded-2xl overflow-hidden bg-black"
            onClick={() => setShowVideo(false)}
          >
            <video
              ref={videoRef}
              src={videoUrl}
              className="w-full h-full object-cover pointer-events-none"
              autoPlay
              playsInline
              onEnded={() => setShowVideo(false)}
            />
            <button
              type="button"
              onClick={() => setShowVideo(false)}
              className="absolute top-3 right-3 w-8 h-8 rounded-full bg-black/50 text-white flex items-center justify-center text-lg"
            >
              ×
            </button>
          </div>
        </div>
      )}
    </>
  )
}
