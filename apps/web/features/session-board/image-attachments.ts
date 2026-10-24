import {z} from "zod";

export const IMAGE_ACCEPT="image/png,image/jpeg,image/webp,image/gif";
export const MAX_IMAGES=6;
export const MAX_IMAGE_BYTES=8*1024*1024;

export function validateImages(files:File[],existing:File[]):string|null{
  if(files.length+existing.length>MAX_IMAGES)return "最多添加 6 张图片";
  if(files.some(file=>!IMAGE_ACCEPT.split(",").includes(file.type)))return "只支持 PNG、JPEG、WebP 和 GIF 图片";
  if(files.some(file=>file.size>MAX_IMAGE_BYTES))return "单张图片不能超过 8 MB";
  if([...files,...existing].reduce((sum,file)=>sum+file.size,0)>32*1024*1024)return "图片总大小不能超过 32 MB";
  return null;
}

export function transferImageFiles(transfer:DataTransfer):File[]{
  const files=Array.from(transfer.files||[]);
  if(files.length)return files;
  return Array.from(transfer.items||[]).filter(item=>item.kind==="file").map(item=>item.getAsFile()).filter((file):file is File=>file!==null);
}

const imageURLs=z.array(z.string().regex(/^http:\/\/127\.0\.0\.1:19514\/codex\/attachments\/[0-9a-f-]{36}\/[1-6]\.(png|jpg|webp|gif)$/)).catch([]);
export function transcriptImageURLs(value:unknown):string[]{return imageURLs.parse(value);}
