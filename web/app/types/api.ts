export type SourceType = 'wechat' | 'web';
export type ArticleStatus = 'failed' | 'pending' | 'processing' | 'ready';
export interface ContentBlock { alt?: string; assetId?: number; level?: number; sourceUrl?: string; text?: string; type: 'code' | 'heading' | 'image' | 'list' | 'paragraph' | 'quote'; }
export interface Asset { id: number; mediaUrl: string; isCover: boolean; mediaType: string; byteSize: number; downloadStatus: 'completed' | 'failed' | 'pending'; }
export interface Article { assets?: Asset[]; author: string; blocks?: ContentBlock[]; canonicalUrl: string; createdAt: string; id: number; originalUrl: string; publishedAt?: string; sourceName: string; sourceType: SourceType; status: ArticleStatus; title: string; }
export interface PageResult<T> { items: T[]; page: number; pageSize: number; total: number; }
export interface DashboardData { articlesTotal: number; collectedToday: number; failedJobs: number; processingJobs: number; recentArticles: Article[]; sourceWechat: number; sourceWeb: number; }
export interface UserInfo { avatar: string; realName: string; roles: string[]; userId: string; username: string; }
export interface Envelope<T> { code: number; data: T; message: string; }
