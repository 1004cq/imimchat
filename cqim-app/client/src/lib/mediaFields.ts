/** Pull file-encryption metadata out of a decrypted extra blob. */
export interface EncryptedMediaFields {
  imageUrl?: string;
  videoUrl?: string;
  voiceUrl?: string;
  fileKey?: string;
  iv?: string;
  isEncryptedMedia?: boolean;
  duration?: number;
  stickerUrl?: string;
  stickerEmoji?: string;
  stickerSetName?: string;
  locationData?: unknown;
  mentions?: string[];
}

export function extractMediaFields(extra?: unknown): EncryptedMediaFields {
  if (!extra || typeof extra !== 'object') return {};
  const value = extra as Record<string, unknown>;
  const str = (key: string) => typeof value[key] === 'string' ? value[key] as string : undefined;
  return {
    imageUrl: str('imageUrl'),
    videoUrl: str('videoUrl'),
    voiceUrl: str('voiceUrl'),
    fileKey: str('fileKey'),
    iv: str('iv'),
    isEncryptedMedia: value.isEncryptedMedia === true,
    duration: typeof value.duration === 'number' ? value.duration : undefined,
    stickerUrl: str('stickerUrl'),
    stickerEmoji: str('stickerEmoji'),
    stickerSetName: str('stickerSetName'),
    locationData: value.locationData,
    mentions: Array.isArray(value.mentions) ? value.mentions as string[] : undefined,
  };
}

export function messageMediaPatch(extra?: unknown): Record<string, unknown> {
  const fields = extractMediaFields(extra);
  const patch: Record<string, unknown> = {};
  if (fields.imageUrl) patch.imageUrl = fields.imageUrl;
  if (fields.videoUrl) patch.videoUrl = fields.videoUrl;
  if (fields.voiceUrl) {
    patch.voiceUrl = fields.voiceUrl;
    patch.duration = fields.duration || 0;
  }
  if (fields.fileKey) patch.fileKey = fields.fileKey;
  if (fields.iv) patch.iv = fields.iv;
  if (fields.stickerUrl) {
    patch.stickerUrl = fields.stickerUrl;
    patch.stickerEmoji = fields.stickerEmoji;
    patch.stickerSetName = fields.stickerSetName;
  }
  if (fields.locationData) patch.locationData = fields.locationData;
  if (fields.mentions) patch.mentions = fields.mentions;
  return patch;
}
