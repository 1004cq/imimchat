/**
 * 在 canvas 上绘制带中心品牌 logo 的二维码（高纠错 H，便于遮挡中心）。
 */
const QR_CENTER_LOGO_SRC = '/imim-qr-center-logo.png';

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error(`failed to load ${src}`));
    img.src = src;
  });
}

function roundRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
) {
  const radius = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + radius, y);
  ctx.arcTo(x + w, y, x + w, y + h, radius);
  ctx.arcTo(x + w, y + h, x, y + h, radius);
  ctx.arcTo(x, y + h, x, y, radius);
  ctx.arcTo(x, y, x + w, y, radius);
  ctx.closePath();
}

export async function drawQrWithCenterLogo(
  canvas: HTMLCanvasElement,
  data: string,
  options?: {
    size?: number;
    dark?: string;
    light?: string;
    logoSrc?: string;
    /** logo 相对边长比例，默认约 22% */
    logoRatio?: number;
  },
): Promise<void> {
  const size = options?.size ?? 220;
  const dark = options?.dark ?? '#1a1a1a';
  const light = options?.light ?? '#ffffff';
  const logoSrc = options?.logoSrc ?? QR_CENTER_LOGO_SRC;
  const logoRatio = options?.logoRatio ?? 0.22;

  const QRCodeMod = await import('qrcode');
  const QRCode = (QRCodeMod as any).default || QRCodeMod;

  await QRCode.toCanvas(canvas, data, {
    width: size,
    margin: 2,
    color: { dark, light },
    errorCorrectionLevel: 'H',
  });

  try {
    const logo = await loadImage(logoSrc);
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const logoSize = Math.round(size * logoRatio);
    const pad = Math.max(4, Math.round(logoSize * 0.12));
    const x = (canvas.width - logoSize) / 2;
    const y = (canvas.height - logoSize) / 2;
    ctx.fillStyle = '#ffffff';
    roundRect(ctx, x - pad, y - pad, logoSize + pad * 2, logoSize + pad * 2, Math.round(pad * 1.2));
    ctx.fill();
    // 圆角裁剪 logo
    ctx.save();
    roundRect(ctx, x, y, logoSize, logoSize, Math.round(logoSize * 0.12));
    ctx.clip();
    ctx.drawImage(logo, x, y, logoSize, logoSize);
    ctx.restore();
  } catch (err) {
    console.warn('[qr] center logo skipped:', err);
  }
}

export { QR_CENTER_LOGO_SRC };
