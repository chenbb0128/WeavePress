import { requestClient } from '#/api/request';

export type SourceType = 'web' | 'wechat';
export type ArticleStatus = 'failed' | 'pending' | 'processing' | 'ready';
export type JobStatus =
  | 'completed'
  | 'completed_with_warnings'
  | 'failed'
  | 'fetching'
  | 'parsing'
  | 'queued'
  | 'storing_assets';

export interface ContentBlock {
  alt?: string;
  assetId?: number;
  level?: number;
  sourceUrl?: string;
  text?: string;
  type: 'code' | 'heading' | 'image' | 'list' | 'paragraph' | 'quote';
}
export interface Asset {
  byteSize: number;
  downloadStatus: 'completed' | 'failed' | 'pending';
  height: number;
  id: number;
  isCover: boolean;
  mediaType: string;
  mediaUrl: string;
  position: number;
  width: number;
}
export interface Article {
  assets?: Asset[];
  author: string;
  blocks?: ContentBlock[];
  canonicalUrl: string;
  createdAt: string;
  duplicateOfId?: number;
  id: number;
  language: string;
  originalUrl: string;
  plainText?: string;
  publishedAt?: string;
  rawSnapshotUrl?: string;
  sourceName: string;
  sourceType: SourceType;
  status: ArticleStatus;
  title: string;
  updatedAt: string;
}
export interface JobEvent {
  createdAt: string;
  id: number;
  message: string;
  status: JobStatus;
}
export interface CollectionJob {
  article?: Article;
  articleId: number;
  attempts: number;
  createdAt: string;
  errorCode?: string;
  errorMessage?: string;
  events?: JobEvent[];
  finishedAt?: string;
  id: number;
  manualRetries: number;
  startedAt?: string;
  status: JobStatus;
  updatedAt: string;
  warnings: string[];
}
export interface PageResult<T> {
  items: T[];
  page: number;
  pageSize: number;
  total: number;
}
export interface DashboardData {
  articlesTotal: number;
  collectedToday: number;
  failedJobs: number;
  processingJobs: number;
  recentArticles: Article[];
  sourceWechat: number;
  sourceWeb: number;
}

export function getDashboardApi() {
  return requestClient.get<DashboardData>('/dashboard');
}
export function submitCollectionApi(url: string) {
  return requestClient.post<{
    article: Article;
    job: CollectionJob;
    reused: boolean;
  }>('/collection-jobs', { url });
}
export function getCollectionJobsApi(params: {
  page: number;
  pageSize: number;
  status?: JobStatus;
}) {
  return requestClient.get<PageResult<CollectionJob>>('/collection-jobs', {
    params,
  });
}
export function getCollectionJobApi(id: number) {
  return requestClient.get<CollectionJob>(`/collection-jobs/${id}`);
}
export function retryCollectionJobApi(id: number) {
  return requestClient.post<CollectionJob>(`/collection-jobs/${id}/retry`);
}
export function getArticlesApi(params: {
  keyword?: string;
  page: number;
  pageSize: number;
  sourceType?: SourceType;
  status?: ArticleStatus;
}) {
  return requestClient.get<PageResult<Article>>('/articles', { params });
}
export function getArticleApi(id: number) {
  return requestClient.get<Article>(`/articles/${id}`);
}
