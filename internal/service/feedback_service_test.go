package service

import (
	"bytes"
	"strings"
	"testing"

	authdomain "aegis/internal/domain/auth"
	"go.uber.org/zap"
)

// 反馈对客户端可见的每一个新错误码都必须登记在网关错误目录里。
func TestFeedbackErrorCodesAreInGatewayCatalog(t *testing.T) {
	t.Parallel()
	registered := map[int]bool{}
	for _, item := range gatewayErrors {
		registered[item.Code] = true
	}
	for _, code := range []int{
		errCodeFeedbackRateLimited, errCodeTicketAttachmentLimit, errCodeTicketAttachmentUnavailable,
		errCodeFeedbackAttachmentKind, errCodeFeedbackImageUnsupported, errCodeFeedbackAttachmentTooLarge,
		errCodeFeedbackNotFound, errCodeFeedbackCategoryUnavailable,
	} {
		if !registered[code] {
			t.Errorf("反馈错误码 %d 没有登记在网关错误目录（auth_protocol_catalog.go）里", code)
		}
	}
}

func TestCheckFeedbackAttachmentCounts(t *testing.T) {
	t.Parallel()
	if _, _, err := checkFeedbackAttachmentCounts([]int64{1, 2, 3, 4, 5}, nil); errCode(err) != errCodeTicketAttachmentLimit {
		t.Fatalf("5 张图片应被拒，得到 %v", err)
	}
	if _, _, err := checkFeedbackAttachmentCounts(nil, []int64{1, 2, 3}); errCode(err) != errCodeTicketAttachmentLimit {
		t.Fatalf("3 个附件应被拒，得到 %v", err)
	}
	images, files, err := checkFeedbackAttachmentCounts([]int64{1, 1, 2, 3, 4, 4}, []int64{9, 9})
	if err != nil || len(images) != 4 || len(files) != 1 {
		t.Fatalf("重复 ID 应先去重再计数，得到 %v %v %v", images, files, err)
	}
	if _, _, err := checkFeedbackAttachmentCounts([]int64{1}, []int64{1}); errCode(err) != errCodeFeedbackAttachmentKind {
		t.Fatalf("同一 ID 不能既是图片又是附件，得到 %v", err)
	}
}

// 冒充图片、超限在碰存储之前就被拒；真图片过了校验才会走到「存储未启用」。
func TestUploadFeedbackAttachmentValidatesBeforeStorage(t *testing.T) {
	t.Parallel()
	svc := NewTicketService(zap.NewNop(), nil, nil, nil, nil)
	session := &authdomain.Session{UserID: 1, AppID: 1}
	upload := func(kind string, declared string, data []byte, size int64) error {
		_, err := svc.UploadFeedbackAttachment(t.Context(), session, TicketAttachmentInput{
			FileName: "x.png", ContentType: declared, ContentLength: size, Content: bytes.NewReader(data), Kind: kind,
		})
		return err
	}
	png := tinyPNG()
	if err := upload("image", "image/png", []byte(strings.Repeat("not an image ", 10)), 130); errCode(err) != errCodeFeedbackImageUnsupported {
		t.Fatalf("文本冒充图片应被拒，得到 %v", err)
	}
	if err := upload("image", "image/png", append([]byte{}, make([]byte, 64)...), 64); errCode(err) != errCodeFeedbackImageUnsupported {
		t.Fatalf("认不出的字节声明为 image/png 也不算图片，得到 %v", err)
	}
	if err := upload("image", "image/png", png, feedbackMaxImageSize+1); errCode(err) != errCodeFeedbackAttachmentTooLarge {
		t.Fatalf("超过 10MB 的图片应被拒，得到 %v", err)
	}
	if err := upload("file", "", []byte("x"), ticketMaxAttachmentSize+1); errCode(err) != errCodeFeedbackAttachmentTooLarge {
		t.Fatalf("超过 20MB 的附件应被拒，得到 %v", err)
	}
	if err := upload("image", "application/octet-stream", png, int64(len(png))); errCode(err) != 50380 {
		t.Fatalf("真 PNG 应通过校验、走到存储层，得到 %v", err)
	}
}
