export type UploadProgressCallback = (progress: number) => void;

async function uploadFileToMinio(
  file: File,
  sourceOrProgress?: string | UploadProgressCallback,
  onProgress?: UploadProgressCallback,
): Promise<string> {
  const progressCallback = typeof sourceOrProgress === 'function' ? sourceOrProgress : onProgress;
  const source = typeof sourceOrProgress === 'string' ? sourceOrProgress : 'moments';
  return new Promise((resolve, reject) => {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('mediaType', file.type.startsWith('video/') ? 'video' : 'image');
    formData.append('source', source);

    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/media/upload-form');
    const token = localStorage.getItem('user_token');
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && progressCallback) progressCallback(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () => {
      try {
        const data = JSON.parse(xhr.responseText);
        if (xhr.status >= 200 && xhr.status < 300 && data.ok) resolve(data.url);
        else reject(new Error(data.error || '上传失败'));
      } catch {
        reject(new Error('上传响应解析失败'));
      }
    };
    xhr.onerror = () => reject(new Error('网络错误，上传失败'));
    xhr.ontimeout = () => reject(new Error('上传超时，请检查网络后重试'));
    xhr.timeout = 300000;
    xhr.send(formData);
  });
}

export { uploadFileToMinio };
