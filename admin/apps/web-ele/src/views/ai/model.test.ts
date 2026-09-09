import type { AIJobEvent, GenerationInput } from '#/api/ai';

import { describe, expect, it } from 'vitest';

import {
  AI_JOB_STATUS_LABELS,
  AI_JOB_TYPE_LABELS,
  canGenerate,
  canRetry,
  shouldPoll,
  validateGenerationForm,
} from './model';

function validInput(): GenerationInput {
  return {
    additionalInstructions: '',
    angleId: 'A1',
    audience: '产品团队',
    idempotencyKey: 'request-123',
    targetWords: 1000,
    tone: 'professional',
  };
}

describe('ai workbench model', () => {
  it('accepts open-ended job event statuses', () => {
    const event: AIJobEvent = {
      createdAt: '2026-09-09T00:00:00Z',
      id: 1,
      jobId: 2,
      message: 'AI 输出格式无效，正在执行一次格式修复',
      status: 'format_repair',
    };

    expect(event.status).toBe('format_repair');
  });

  it('only polls queued and running jobs', () => {
    expect(shouldPoll('queued')).toBe(true);
    expect(shouldPoll('running')).toBe(true);
    expect(shouldPoll('completed')).toBe(false);
    expect(shouldPoll('failed')).toBe(false);
  });

  it('uses stable Chinese labels', () => {
    expect(AI_JOB_STATUS_LABELS).toEqual({
      completed: '已完成',
      failed: '失败',
      queued: '排队中',
      running: '执行中',
    });
    expect(AI_JOB_TYPE_LABELS).toEqual({
      analysis: '文章分析',
      generation: '稿件生成',
    });
  });

  it('requires completed analysis and valid generation input', () => {
    const input = validInput();

    expect(validateGenerationForm(input)).toEqual([]);
    expect(canGenerate('completed', input)).toBe(true);
    expect(canGenerate('running', input)).toBe(false);
  });

  it('returns field-level Chinese errors for invalid input', () => {
    expect(
      validateGenerationForm({
        additionalInstructions: '补'.repeat(501),
        angleId: '   ',
        audience: '   ',
        idempotencyKey: '中文请求标识',
        targetWords: 299,
        tone: 'sales' as GenerationInput['tone'],
      }),
    ).toEqual([
      { field: 'angleId', message: '请选择文章角度' },
      { field: 'audience', message: '请输入目标读者' },
      { field: 'tone', message: '请选择有效语气' },
      {
        field: 'targetWords',
        message: '目标字数应在 300 到 5000 之间',
      },
      {
        field: 'additionalInstructions',
        message: '补充要求不能超过 500 字',
      },
      {
        field: 'idempotencyKey',
        message: '幂等键只能包含 ASCII 字符',
      },
    ]);
  });

  it('validates Unicode character counts and boundary values', () => {
    expect(
      validateGenerationForm({
        ...validInput(),
        additionalInstructions: '😀'.repeat(500),
        audience: '😀'.repeat(100),
        idempotencyKey: 'x'.repeat(128),
        targetWords: 5000,
      }),
    ).toEqual([]);

    expect(
      validateGenerationForm({
        ...validInput(),
        audience: '😀'.repeat(101),
        idempotencyKey: '1234567',
        targetWords: 5001,
      }),
    ).toEqual([
      {
        field: 'audience',
        message: '目标读者不能超过 100 字',
      },
      {
        field: 'targetWords',
        message: '目标字数应在 300 到 5000 之间',
      },
      {
        field: 'idempotencyKey',
        message: '幂等键长度应在 8 到 128 个字符之间',
      },
    ]);
  });

  it('allows manual retry only for retryable failed jobs', () => {
    expect(canRetry({ retryable: true, status: 'failed' })).toBe(true);
    expect(canRetry({ retryable: false, status: 'failed' })).toBe(false);
    expect(canRetry({ retryable: true, status: 'running' })).toBe(false);
    expect(canRetry({ retryable: true, status: 'completed' })).toBe(false);
  });
});
