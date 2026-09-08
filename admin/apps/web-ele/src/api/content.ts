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
export type DraftStatus =
  | 'approved'
  | 'editing'
  | 'in_review'
  | 'publish_failed'
  | 'published'
  | 'publishing';
export type PublishJobStatus = 'completed' | 'failed' | 'publishing' | 'queued';

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
export interface DraftEvent {
  actorId: number;
  createdAt: string;
  draftId: number;
  fromStatus: string;
  id: number;
  note: string;
  toStatus: DraftStatus;
}
export interface DraftVersion {
  author: string;
  changeNote: string;
  contentHtml: string;
  coverAssetId?: number;
  createdAt: string;
  createdBy: number;
  digest: string;
  draftId: number;
  id: number;
  title: string;
  version: number;
}
export interface Draft {
  author: string;
  contentHtml: string;
  coverAssetId?: number;
  createdAt: string;
  createdBy: number;
  currentVersion: number;
  digest: string;
  events?: DraftEvent[];
  id: number;
  previewHtml?: string;
  sourceArticle?: Article;
  sourceArticleId: number;
  status: DraftStatus;
  title: string;
  updatedAt: string;
  updatedBy: number;
}
export interface PublishJobEvent {
  createdAt: string;
  id: number;
  message: string;
  status: PublishJobStatus;
}
export interface WeChatPublishJob {
  attempts: number;
  createdAt: string;
  draft?: Draft;
  draftId: number;
  errorCode?: string;
  errorMessage?: string;
  events?: PublishJobEvent[];
  finishedAt?: string;
  id: number;
  manualRetries: number;
  remoteMediaId?: string;
  requestedBy: number;
  startedAt?: string;
  status: PublishJobStatus;
  updatedAt: string;
}
export interface WeChatStatus {
  appConfigured: boolean;
  enabled: boolean;
}
export interface PreflightIssue {
  code: string;
  field?: string;
  message: string;
}
export interface PreflightResult {
  issues: PreflightIssue[];
  valid: boolean;
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
export function createDraftApi(articleId: number) {
  return requestClient.post<Draft>('/drafts', { articleId });
}
export function getDraftsApi(params: {
  keyword?: string;
  page: number;
  pageSize: number;
  status?: DraftStatus;
}) {
  return requestClient.get<PageResult<Draft>>('/drafts', { params });
}
export function getDraftApi(id: number) {
  return requestClient.get<Draft>(`/drafts/${id}`);
}
export function updateDraftApi(
  id: number,
  input: {
    author: string;
    changeNote: string;
    contentHtml: string;
    coverAssetId?: number;
    digest: string;
    expectedVersion: number;
    title: string;
  },
) {
  return requestClient.put<Draft>(`/drafts/${id}`, input);
}
export function getDraftVersionsApi(id: number) {
  return requestClient.get<DraftVersion[]>(`/drafts/${id}/versions`);
}
export function restoreDraftVersionApi(
  id: number,
  version: number,
  expectedVersion: number,
) {
  return requestClient.post<Draft>(
    `/drafts/${id}/versions/${version}/restore`,
    { expectedVersion },
  );
}
export function getDraftPreflightApi(id: number) {
  return requestClient.get<PreflightResult>(`/drafts/${id}/preflight`);
}
export function submitDraftReviewApi(id: number) {
  return requestClient.post<Draft>(`/drafts/${id}/submit-review`);
}
export function reviewDraftApi(id: number, approved: boolean, note = '') {
  return requestClient.post<Draft>(`/drafts/${id}/review`, {
    approved,
    note,
  });
}
export function publishDraftApi(id: number) {
  return requestClient.post<WeChatPublishJob>(`/drafts/${id}/publish`);
}
export function getWeChatStatusApi() {
  return requestClient.get<WeChatStatus>('/wechat/status');
}
export function getWeChatPublishJobsApi(params: {
  page: number;
  pageSize: number;
  status?: PublishJobStatus;
}) {
  return requestClient.get<PageResult<WeChatPublishJob>>(
    '/wechat-publish-jobs',
    { params },
  );
}
export function getWeChatPublishJobApi(id: number) {
  return requestClient.get<WeChatPublishJob>(`/wechat-publish-jobs/${id}`);
}
export function retryWeChatPublishJobApi(id: number) {
  return requestClient.post<WeChatPublishJob>(
    `/wechat-publish-jobs/${id}/retry`,
  );
}
