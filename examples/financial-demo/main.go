// GibRAM Demo - Comprehensive example with Indonesian financial data
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/gibram-io/gibram/pkg/client"
	"github.com/gibram-io/gibram/pkg/types"
)

// =============================================================================
// Configuration
// =============================================================================

var (
	addr          = flag.String("addr", "localhost:6161", "Server address")
	apiKey        = flag.String("key", "", "API key for authentication")
	tlsEnabled    = flag.Bool("tls", false, "Enable TLS")
	tlsSkipVerify = flag.Bool("tls-skip-verify", false, "Skip TLS verification")
	seedData      = flag.Bool("seed", true, "Seed demo data")
	queryDemo     = flag.Bool("query", true, "Run query demos")
)

// =============================================================================
// Demo Data - Indonesian Financial Ecosystem
// =============================================================================

// Organizations
var organizations = []struct {
	ID          string
	Name        string
	Description string
}{
	{"org-bi", "BANK INDONESIA", "Bank sentral Republik Indonesia yang bertugas menjaga stabilitas moneter, sistem keuangan, dan sistem pembayaran."},
	{"org-ojk", "OTORITAS JASA KEUANGAN", "Lembaga independen yang mengatur dan mengawasi sektor jasa keuangan di Indonesia."},
	{"org-lps", "LEMBAGA PENJAMIN SIMPANAN", "Lembaga yang menjamin simpanan nasabah bank di Indonesia hingga Rp 2 miliar."},
	{"org-bri", "BANK RAKYAT INDONESIA", "Bank BUMN terbesar di Indonesia dengan fokus pada sektor UMKM dan perbankan mikro."},
	{"org-bca", "BANK CENTRAL ASIA", "Bank swasta terbesar di Indonesia dengan layanan perbankan digital terdepan."},
	{"org-mandiri", "BANK MANDIRI", "Bank BUMN hasil merger empat bank pemerintah dengan aset terbesar di Indonesia."},
	{"org-bni", "BANK NEGARA INDONESIA", "Bank BUMN tertua di Indonesia dengan jaringan internasional yang luas."},
	{"org-btn", "BANK TABUNGAN NEGARA", "Bank BUMN yang fokus pada pembiayaan perumahan dan KPR."},
	{"org-idx", "BURSA EFEK INDONESIA", "Penyelenggara perdagangan efek di pasar modal Indonesia."},
	{"org-ksei", "KUSTODIAN SENTRAL EFEK INDONESIA", "Lembaga penyimpanan dan penyelesaian transaksi efek di Indonesia."},
	{"org-gopay", "GOPAY", "Dompet digital terbesar di Indonesia yang terintegrasi dengan ekosistem Gojek."},
	{"org-ovo", "OVO", "Platform pembayaran digital dengan jaringan merchant terluas di Indonesia."},
	{"org-dana", "DANA", "Dompet digital dengan fitur transfer antar bank tanpa biaya."},
	{"org-shopeepay", "SHOPEEPAY", "Layanan pembayaran digital terintegrasi dengan e-commerce Shopee."},
	{"org-flip", "FLIP", "Startup fintech untuk transfer antar bank tanpa biaya."},
	{"org-akulaku", "AKULAKU", "Platform paylater dan pinjaman online terkemuka di Indonesia."},
	{"org-kredivo", "KREDIVO", "Layanan paylater dengan limit kredit hingga Rp 30 juta."},
	{"org-amartha", "AMARTHA", "Platform P2P lending yang fokus pada pemberdayaan UMKM perempuan."},
	{"org-modalku", "MODALKU", "Platform P2P lending untuk pembiayaan UMKM di Indonesia."},
	{"org-investree", "INVESTREE", "Platform P2P lending dengan fokus invoice financing."},
}

// People
var people = []struct {
	ID          string
	Name        string
	Description string
}{
	{"per-perry", "PERRY WARJIYO", "Gubernur Bank Indonesia periode 2018-2028, ahli ekonomi moneter."},
	{"per-mahendra", "MAHENDRA SIREGAR", "Ketua Dewan Komisioner OJK periode 2022-2027."},
	{"per-purbaya", "PURBAYA YUDHI SADEWA", "Kepala Eksekutif Pengawas Perbankan OJK."},
	{"per-sunarso", "SUNARSO", "Direktur Utama BRI periode 2019-2024."},
	{"per-jahja", "JAHJA SETIAATMADJA", "Presiden Direktur BCA sejak 2011."},
	{"per-darmawan", "DARMAWAN JUNAIDI", "Direktur Utama Bank Mandiri periode 2020-2025."},
	{"per-royke", "ROYKE TUMILAAR", "Direktur Utama BNI periode 2020-2025."},
	{"per-haru", "HARU KOESMAHARGYO", "Direktur Utama BTN periode 2020-2025."},
	{"per-inarno", "INARNO DJAJADI", "Direktur Utama BEI periode 2019-2024."},
	{"per-nadiem", "NADIEM MAKARIM", "Founder Gojek dan GoPay, Menteri Pendidikan RI."},
	{"per-jason", "JASON THOMPSON", "CEO OVO dan Grab Financial Group Indonesia."},
	{"per-vince", "VINCENT ISWARA", "CEO DANA Indonesia."},
	{"per-akshay", "AKSHAY GARG", "CEO Kredivo Group."},
}

// Concepts
var concepts = []struct {
	ID          string
	Name        string
	Description string
}{
	{"con-bi-rate", "BI-RATE", "Suku bunga acuan Bank Indonesia yang mempengaruhi suku bunga perbankan."},
	{"con-inflasi", "INFLASI", "Kenaikan harga barang dan jasa secara umum dan terus menerus."},
	{"con-rupiah", "RUPIAH", "Mata uang resmi Republik Indonesia dengan kode IDR."},
	{"con-qris", "QRIS", "QR Code Indonesian Standard untuk pembayaran digital terintegrasi."},
	{"con-bi-fast", "BI-FAST", "Sistem pembayaran ritel real-time Bank Indonesia 24/7."},
	{"con-sknbi", "SKNBI", "Sistem Kliring Nasional Bank Indonesia untuk transfer antar bank."},
	{"con-rtgs", "RTGS", "Real Time Gross Settlement untuk transfer dana besar."},
	{"con-ldr", "LOAN TO DEPOSIT RATIO", "Rasio pinjaman terhadap simpanan bank."},
	{"con-car", "CAPITAL ADEQUACY RATIO", "Rasio kecukupan modal bank minimum 8%."},
	{"con-npl", "NON PERFORMING LOAN", "Rasio kredit bermasalah bank."},
	{"con-nim", "NET INTEREST MARGIN", "Selisih pendapatan bunga dan beban bunga bank."},
	{"con-ihsg", "IHSG", "Indeks Harga Saham Gabungan di Bursa Efek Indonesia."},
	{"con-lq45", "LQ45", "Indeks 45 saham paling likuid di BEI."},
	{"con-obligasi", "OBLIGASI", "Surat utang jangka panjang yang diterbitkan pemerintah atau korporasi."},
	{"con-sbr", "SBR", "Surat Berharga Ritel yang diterbitkan pemerintah untuk investor individu."},
	{"con-reksadana", "REKSA DANA", "Wadah investasi kolektif yang dikelola manajer investasi."},
	{"con-paylater", "PAYLATER", "Layanan kredit instan untuk pembayaran di kemudian hari."},
	{"con-p2p", "P2P LENDING", "Peer-to-peer lending platform untuk pinjaman langsung."},
	{"con-e-money", "E-MONEY", "Uang elektronik yang tersimpan dalam media elektronik."},
	{"con-dompet-digital", "DOMPET DIGITAL", "Aplikasi penyimpanan uang elektronik di smartphone."},
}

// Regulations
var regulations = []struct {
	ID          string
	Name        string
	Description string
}{
	{"reg-pbi-emoney", "PBI UANG ELEKTRONIK", "Peraturan Bank Indonesia tentang penyelenggaraan uang elektronik."},
	{"reg-pojk-p2p", "POJK P2P LENDING", "Peraturan OJK tentang layanan pinjam meminjam uang berbasis teknologi."},
	{"reg-pojk-bank", "POJK PERBANKAN DIGITAL", "Peraturan OJK tentang bank digital dan layanan perbankan berbasis teknologi."},
	{"reg-pbi-qris", "PBI QRIS", "Peraturan Bank Indonesia tentang standar QR Code pembayaran."},
	{"reg-pojk-equity", "POJK EQUITY CROWDFUNDING", "Peraturan OJK tentang layanan urun dana berbasis teknologi."},
	{"reg-uu-bi", "UU BANK INDONESIA", "Undang-undang yang mengatur tugas dan wewenang Bank Indonesia."},
	{"reg-uu-perbankan", "UU PERBANKAN", "Undang-undang yang mengatur kegiatan usaha perbankan di Indonesia."},
	{"reg-uu-ojk", "UU OJK", "Undang-undang pembentukan Otoritas Jasa Keuangan."},
	{"reg-uu-lps", "UU LPS", "Undang-undang tentang Lembaga Penjamin Simpanan."},
	{"reg-uu-pm", "UU PASAR MODAL", "Undang-undang yang mengatur pasar modal Indonesia."},
}

// Events/News
var events = []struct {
	ID          string
	Name        string
	Description string
}{
	{"evt-bi-rate-2024", "KEPUTUSAN BI-RATE JANUARI 2024", "Bank Indonesia mempertahankan BI-Rate di level 6% untuk menjaga stabilitas Rupiah."},
	{"evt-ihsg-ath", "IHSG ALL TIME HIGH 2024", "IHSG mencapai level tertinggi sepanjang masa di 7.500 pada Februari 2024."},
	{"evt-qris-lintas", "QRIS LINTAS NEGARA", "Peluncuran QRIS untuk transaksi lintas negara Indonesia-Thailand-Malaysia."},
	{"evt-bi-fast-launch", "PELUNCURAN BI-FAST", "Bank Indonesia meluncurkan sistem pembayaran real-time 24/7 BI-FAST."},
	{"evt-ojk-moratorium", "MORATORIUM P2P", "OJK menghentikan sementara izin baru fintech P2P lending."},
	{"evt-bank-digital", "ERA BANK DIGITAL", "OJK menerbitkan izin untuk 5 bank digital baru di Indonesia."},
	{"evt-merger-gojek", "MERGER GOJEK-TOKOPEDIA", "Penggabungan Gojek dan Tokopedia membentuk GoTo Group."},
	{"evt-ipo-goto", "IPO GOTO", "GoTo Group melantai di BEI dengan valuasi Rp 400 triliun."},
	{"evt-inflasi-2024", "INFLASI TERKENDALI 2024", "Inflasi Indonesia terkendali di level 3.2% tahun 2024."},
	{"evt-rupiah-stabil", "STABILITAS RUPIAH", "Rupiah stabil di kisaran Rp 15.500 per USD sepanjang 2024."},
}

// Relationships between entities
var relationships = []struct {
	SourceID    string
	TargetID    string
	RelType     string
	Description string
	Weight      float32
}{
	// Organizational relationships
	{"per-perry", "org-bi", "GOVERNOR_OF", "Perry Warjiyo adalah Gubernur Bank Indonesia", 1.0},
	{"per-mahendra", "org-ojk", "CHAIRMAN_OF", "Mahendra Siregar adalah Ketua OJK", 1.0},
	{"per-sunarso", "org-bri", "CEO_OF", "Sunarso adalah Direktur Utama BRI", 1.0},
	{"per-jahja", "org-bca", "CEO_OF", "Jahja Setiaatmadja adalah Presiden Direktur BCA", 1.0},
	{"per-darmawan", "org-mandiri", "CEO_OF", "Darmawan Junaidi adalah Direktur Utama Mandiri", 1.0},
	{"per-royke", "org-bni", "CEO_OF", "Royke Tumilaar adalah Direktur Utama BNI", 1.0},
	{"per-haru", "org-btn", "CEO_OF", "Haru Koesmahargyo adalah Direktur Utama BTN", 1.0},
	{"per-inarno", "org-idx", "CEO_OF", "Inarno Djajadi adalah Direktur Utama BEI", 1.0},
	{"per-nadiem", "org-gopay", "FOUNDER_OF", "Nadiem Makarim adalah founder GoPay", 1.0},
	{"per-vince", "org-dana", "CEO_OF", "Vincent Iswara adalah CEO DANA", 1.0},
	{"per-akshay", "org-kredivo", "CEO_OF", "Akshay Garg adalah CEO Kredivo", 1.0},

	// Regulatory relationships
	{"org-bi", "con-bi-rate", "SETS", "BI menetapkan suku bunga acuan BI-Rate", 1.0},
	{"org-bi", "con-rupiah", "MANAGES", "BI mengelola stabilitas nilai tukar Rupiah", 1.0},
	{"org-bi", "con-qris", "OPERATES", "BI mengoperasikan standar QRIS", 1.0},
	{"org-bi", "con-bi-fast", "OPERATES", "BI mengoperasikan sistem BI-FAST", 1.0},
	{"org-bi", "con-rtgs", "OPERATES", "BI mengoperasikan sistem RTGS", 1.0},
	{"org-bi", "con-sknbi", "OPERATES", "BI mengoperasikan sistem SKNBI", 1.0},
	{"org-ojk", "org-bri", "SUPERVISES", "OJK mengawasi Bank BRI", 0.9},
	{"org-ojk", "org-bca", "SUPERVISES", "OJK mengawasi Bank BCA", 0.9},
	{"org-ojk", "org-mandiri", "SUPERVISES", "OJK mengawasi Bank Mandiri", 0.9},
	{"org-ojk", "org-bni", "SUPERVISES", "OJK mengawasi Bank BNI", 0.9},
	{"org-ojk", "org-idx", "SUPERVISES", "OJK mengawasi Bursa Efek Indonesia", 0.9},
	{"org-ojk", "org-kredivo", "SUPERVISES", "OJK mengawasi Kredivo", 0.8},
	{"org-ojk", "org-akulaku", "SUPERVISES", "OJK mengawasi Akulaku", 0.8},
	{"org-lps", "org-bri", "GUARANTEES", "LPS menjamin simpanan nasabah BRI", 0.9},
	{"org-lps", "org-bca", "GUARANTEES", "LPS menjamin simpanan nasabah BCA", 0.9},

	// Business relationships
	{"org-gopay", "con-qris", "USES", "GoPay menggunakan QRIS untuk pembayaran", 0.8},
	{"org-ovo", "con-qris", "USES", "OVO menggunakan QRIS untuk pembayaran", 0.8},
	{"org-dana", "con-qris", "USES", "DANA menggunakan QRIS untuk pembayaran", 0.8},
	{"org-shopeepay", "con-qris", "USES", "ShopeePay menggunakan QRIS untuk pembayaran", 0.8},
	{"org-gopay", "con-e-money", "PROVIDES", "GoPay menyediakan layanan e-money", 0.9},
	{"org-ovo", "con-e-money", "PROVIDES", "OVO menyediakan layanan e-money", 0.9},
	{"org-dana", "con-e-money", "PROVIDES", "DANA menyediakan layanan e-money", 0.9},
	{"org-kredivo", "con-paylater", "PROVIDES", "Kredivo menyediakan layanan paylater", 0.9},
	{"org-akulaku", "con-paylater", "PROVIDES", "Akulaku menyediakan layanan paylater", 0.9},
	{"org-amartha", "con-p2p", "OPERATES", "Amartha mengoperasikan platform P2P lending", 0.9},
	{"org-modalku", "con-p2p", "OPERATES", "Modalku mengoperasikan platform P2P lending", 0.9},
	{"org-investree", "con-p2p", "OPERATES", "Investree mengoperasikan platform P2P lending", 0.9},

	// Market relationships
	{"org-idx", "con-ihsg", "CALCULATES", "BEI menghitung IHSG", 1.0},
	{"org-idx", "con-lq45", "CALCULATES", "BEI menghitung indeks LQ45", 1.0},
	{"org-bri", "org-idx", "LISTED_ON", "BRI tercatat di BEI", 0.9},
	{"org-bca", "org-idx", "LISTED_ON", "BCA tercatat di BEI", 0.9},
	{"org-mandiri", "org-idx", "LISTED_ON", "Mandiri tercatat di BEI", 0.9},
	{"org-bni", "org-idx", "LISTED_ON", "BNI tercatat di BEI", 0.9},
	{"org-btn", "org-idx", "LISTED_ON", "BTN tercatat di BEI", 0.9},

	// Banking metrics
	{"org-bri", "con-ldr", "MEASURED_BY", "LDR BRI dijaga pada level optimal", 0.7},
	{"org-bri", "con-car", "MEASURED_BY", "CAR BRI di atas ketentuan minimum", 0.7},
	{"org-bri", "con-npl", "MEASURED_BY", "NPL BRI terjaga rendah", 0.7},
	{"org-bca", "con-nim", "MEASURED_BY", "NIM BCA tertinggi di industri", 0.7},

	// Regulatory frameworks
	{"reg-pbi-emoney", "con-e-money", "REGULATES", "PBI mengatur uang elektronik", 0.9},
	{"reg-pojk-p2p", "con-p2p", "REGULATES", "POJK mengatur P2P lending", 0.9},
	{"reg-pbi-qris", "con-qris", "REGULATES", "PBI mengatur standar QRIS", 0.9},
	{"reg-uu-bi", "org-bi", "GOVERNS", "UU BI mengatur Bank Indonesia", 1.0},
	{"reg-uu-ojk", "org-ojk", "GOVERNS", "UU OJK mengatur OJK", 1.0},
	{"reg-uu-lps", "org-lps", "GOVERNS", "UU LPS mengatur LPS", 1.0},
	{"reg-uu-pm", "org-idx", "GOVERNS", "UU PM mengatur pasar modal", 1.0},

	// Events relationships
	{"evt-bi-rate-2024", "con-bi-rate", "RELATES_TO", "Keputusan tentang BI-Rate", 0.9},
	{"evt-bi-rate-2024", "org-bi", "ANNOUNCED_BY", "Diumumkan oleh Bank Indonesia", 0.9},
	{"evt-ihsg-ath", "con-ihsg", "RELATES_TO", "Pencapaian IHSG tertinggi", 0.9},
	{"evt-ihsg-ath", "org-idx", "OCCURRED_AT", "Terjadi di Bursa Efek Indonesia", 0.9},
	{"evt-qris-lintas", "con-qris", "RELATES_TO", "Ekspansi QRIS lintas negara", 0.9},
	{"evt-bi-fast-launch", "con-bi-fast", "RELATES_TO", "Peluncuran sistem BI-FAST", 0.9},
	{"evt-inflasi-2024", "con-inflasi", "RELATES_TO", "Data inflasi 2024", 0.9},
	{"evt-rupiah-stabil", "con-rupiah", "RELATES_TO", "Stabilitas nilai tukar Rupiah", 0.9},
}

// Text content for chunks
var textContents = []struct {
	DocID   string
	ChunkID string
	Content string
}{
	// Bank Indonesia documents
	{"doc-bi-policy", "chunk-bi-1", "Bank Indonesia (BI) pada Rapat Dewan Gubernur (RDG) Januari 2024 memutuskan untuk mempertahankan BI-Rate pada level 6,00%, suku bunga Deposit Facility pada level 5,25%, dan suku bunga Lending Facility pada level 6,75%. Keputusan ini konsisten dengan stance kebijakan moneter untuk menjaga stabilitas nilai tukar Rupiah dari dampak ketidakpastian pasar keuangan global serta sebagai langkah pre-emptive dan forward looking untuk memastikan inflasi tetap terkendali dalam sasaran 2,5±1% pada 2024."},
	{"doc-bi-policy", "chunk-bi-2", "Gubernur Bank Indonesia Perry Warjiyo menyampaikan bahwa stabilitas Rupiah terus terjaga didukung oleh kebijakan stabilisasi nilai tukar yang ditempuh Bank Indonesia, baik melalui intervensi di pasar valas maupun instrumen SRBI dan SVBI. Nilai tukar Rupiah pada Januari 2024 menguat 0,5% dibandingkan level akhir Desember 2023. Penguatan ini ditopang oleh persepsi positif investor terhadap prospek ekonomi Indonesia dan diferensial suku bunga yang menarik."},
	{"doc-bi-policy", "chunk-bi-3", "Bank Indonesia terus memperkuat infrastruktur sistem pembayaran digital melalui pengembangan QRIS dan BI-FAST. Hingga Desember 2023, jumlah pengguna QRIS mencapai 45 juta dengan 25 juta merchant. Transaksi BI-FAST mencapai Rp 150 triliun per bulan dengan waktu settlement kurang dari 5 detik. Interkoneksi QRIS lintas negara dengan Thailand dan Malaysia telah beroperasi sejak Agustus 2023."},

	// OJK documents
	{"doc-ojk-report", "chunk-ojk-1", "Otoritas Jasa Keuangan (OJK) mencatat industri perbankan nasional tetap solid dengan Capital Adequacy Ratio (CAR) perbankan pada level 25,48% per Desember 2023, jauh di atas ketentuan minimum 8%. Non Performing Loan (NPL) gross tercatat 2,35% dan NPL net 0,81%, menunjukkan kualitas kredit yang terjaga baik. Likuiditas perbankan juga memadai dengan Loan to Deposit Ratio (LDR) pada level 84,51%."},
	{"doc-ojk-report", "chunk-ojk-2", "Sektor fintech peer-to-peer (P2P) lending menunjukkan pertumbuhan signifikan dengan outstanding pinjaman mencapai Rp 57,89 triliun per Desember 2023, naik 15,3% year-on-year. Jumlah peminjam tercatat 19,76 juta entitas dengan tingkat keberhasilan bayar 97% (TKB90). OJK terus memperketat pengawasan terhadap fintech lending untuk melindungi konsumen dan menjaga stabilitas sistem keuangan."},
	{"doc-ojk-report", "chunk-ojk-3", "Ketua Dewan Komisioner OJK Mahendra Siregar menegaskan komitmen OJK dalam mendorong transformasi digital perbankan. OJK telah menerbitkan izin untuk 5 bank digital baru dan terus mengembangkan regulatory sandbox untuk inovasi keuangan digital. Fokus utama adalah keamanan siber, perlindungan data konsumen, dan inklusi keuangan melalui teknologi."},

	// Market documents
	{"doc-market", "chunk-market-1", "Indeks Harga Saham Gabungan (IHSG) mencatat kinerja positif sepanjang 2024 dengan mencapai level tertinggi sepanjang masa di 7.500 pada Februari 2024. Kapitalisasi pasar Bursa Efek Indonesia mencapai Rp 11.500 triliun. Saham-saham perbankan menjadi penopang utama dengan kontribusi 30% terhadap pergerakan IHSG. Bank BCA, Bank BRI, dan Bank Mandiri masuk dalam 10 besar saham dengan kapitalisasi pasar terbesar."},
	{"doc-market", "chunk-market-2", "Volume transaksi harian rata-rata di BEI mencapai Rp 12 triliun dengan jumlah investor aktif 4,5 juta Single Investor Identification (SID). Indeks LQ45 yang berisi 45 saham paling likuid mencatat return 12% year-to-date. Sektor keuangan dan teknologi menjadi sektor dengan kinerja terbaik, didorong oleh prospek pertumbuhan ekonomi digital Indonesia."},

	// Fintech documents
	{"doc-fintech", "chunk-fintech-1", "Ekosistem pembayaran digital Indonesia terus berkembang pesat dengan GoPay, OVO, DANA, dan ShopeePay sebagai pemain utama. Total transaksi e-money mencapai Rp 450 triliun pada 2023, naik 25% dari tahun sebelumnya. QRIS menjadi standar pembayaran yang mengintegrasikan seluruh e-wallet dan perbankan dengan lebih dari 25 juta merchant terdaftar."},
	{"doc-fintech", "chunk-fintech-2", "Layanan paylater atau Buy Now Pay Later (BNPL) mengalami pertumbuhan eksponensial dengan pemain utama Kredivo, Akulaku, dan layanan paylater dari e-commerce. Total outstanding paylater mencapai Rp 25 triliun dengan 15 juta pengguna aktif. OJK terus memperketat regulasi untuk memastikan praktik pemberian kredit yang bertanggung jawab dan perlindungan konsumen."},
	{"doc-fintech", "chunk-fintech-3", "Platform P2P lending seperti Amartha, Modalku, dan Investree berperan penting dalam pembiayaan UMKM. Amartha fokus pada pemberdayaan perempuan pengusaha di pedesaan dengan model group lending. Modalku dan Investree melayani UMKM perkotaan dengan produk invoice financing dan term loan. Total penyaluran kredit P2P lending ke UMKM mencapai Rp 40 triliun pada 2023."},

	// Banking digital transformation
	{"doc-banking", "chunk-banking-1", "Bank-bank besar Indonesia berlomba dalam transformasi digital. BCA meluncurkan myBCA sebagai super app dengan 30 juta pengguna aktif bulanan. BRI mengembangkan BRImo dengan penetrasi ke segmen unbanked melalui agen BRILink. Mandiri Livin mencapai 20 juta pengguna dengan fitur investasi terintegrasi. BNI Mobile Banking fokus pada UMKM digital dengan layanan pembukaan rekening online."},
	{"doc-banking", "chunk-banking-2", "Era bank digital ditandai dengan kemunculan Bank Jago, Allo Bank, Bank Neo Commerce, dan SeaBank. Bank-bank digital ini menawarkan pengalaman perbankan mobile-first dengan bunga kompetitif dan tanpa biaya administrasi. Kolaborasi antara bank digital dengan ekosistem e-commerce dan fintech menjadi kunci pertumbuhan. SeaBank terintegrasi dengan Shopee, sementara Allo Bank bermitra dengan Bukalapak."},

	// Regulatory updates
	{"doc-regulation", "chunk-reg-1", "Peraturan Bank Indonesia tentang Uang Elektronik (PBI UE) mengatur limit saldo dan transaksi e-money. E-money terdaftar memiliki limit saldo Rp 2 juta, sedangkan e-money teridentiikasi memiliki limit Rp 20 juta. Penyelenggara e-money wajib memiliki izin dari Bank Indonesia dan memenuhi persyaratan modal minimum Rp 3 miliar untuk non-bank."},
	{"doc-regulation", "chunk-reg-2", "POJK tentang Layanan Pinjam Meminjam Uang Berbasis Teknologi Informasi mengatur bahwa fintech P2P lending wajib memiliki izin dari OJK. Batas maksimum suku bunga ditetapkan 0,4% per hari untuk pinjaman produktif dan 0,3% untuk pinjaman konsumtif. Penyelenggara wajib melaporkan data ke Fintech Data Center dan bergabung dengan Asosiasi Fintech Indonesia (AFTECH)."},
}

// =============================================================================
// Main
// =============================================================================

func main() {
	flag.Parse()

	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║        GibRAM Demo - Indonesian Financial Ecosystem            ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Build config
	cfg := client.DefaultPoolConfig()
	cfg.TLSEnabled = *tlsEnabled
	cfg.TLSSkipVerify = *tlsSkipVerify
	cfg.APIKey = *apiKey

	// Connect (auth happens automatically if API key is set)
	fmt.Printf("🔌 Connecting to %s...\n", *addr)
	c, err := client.NewClientWithConfig(*addr, "default-session", cfg)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Printf("Close error: %v", err)
		}
	}()

	if *apiKey != "" {
		fmt.Println("✓ Authenticated (API key provided)")
	}

	// Verify connection
	info, err := c.Info()
	if err != nil {
		log.Fatalf("Failed to get info: %v", err)
	}
	fmt.Printf("✓ Connected to GibRAM %s (vector_dim=%d)\n", info.Version, info.VectorDim)
	fmt.Println()

	if *seedData {
		seedDemoData(c, info.VectorDim)
	}

	if *queryDemo {
		runQueryDemos(c, info.VectorDim)
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("                      Demo Complete!                            ")
	fmt.Println("═══════════════════════════════════════════════════════════════")
}

// =============================================================================
// Data Seeding
// =============================================================================

func seedDemoData(c *client.Client, vectorDim int) {
	fmt.Println("📦 SEEDING DEMO DATA")
	fmt.Println("───────────────────────────────────────────────────────────────")

	entityIDs := make(map[string]uint64)
	var totalEntities, totalRelationships, totalTextUnits int

	// 1. Add Documents
	fmt.Println("\n📄 Adding documents...")
	docIDs := make(map[string]uint64)
	docs := []string{"doc-bi-policy", "doc-ojk-report", "doc-market", "doc-fintech", "doc-banking", "doc-regulation"}
	for _, docID := range docs {
		id, err := c.AddDocument(docID, docID+".pdf")
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				fmt.Printf("  • %s (already exists)\n", docID)
				continue
			}
			log.Printf("  ✗ Failed to add document %s: %v", docID, err)
			continue
		}
		docIDs[docID] = id
		fmt.Printf("  ✓ %s (id=%d)\n", docID, id)
	}

	// 2. Add Organizations
	fmt.Println("\n🏢 Adding organizations...")
	for _, org := range organizations {
		embedding := generateSemanticEmbedding(org.Name+" "+org.Description, vectorDim)
		id, err := c.AddEntity(org.ID, org.Name, "organization", org.Description, embedding)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				// Get existing entity ID
				ent, _ := c.GetEntityByTitle(org.Name)
				if ent != nil {
					entityIDs[org.ID] = ent.ID
				}
				continue
			}
			log.Printf("  ✗ Failed to add org %s: %v", org.Name, err)
			continue
		}
		entityIDs[org.ID] = id
		totalEntities++
		fmt.Printf("  ✓ %s (id=%d)\n", org.Name, id)
	}

	// 3. Add People
	fmt.Println("\n👤 Adding people...")
	for _, person := range people {
		embedding := generateSemanticEmbedding(person.Name+" "+person.Description, vectorDim)
		id, err := c.AddEntity(person.ID, person.Name, "person", person.Description, embedding)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				ent, _ := c.GetEntityByTitle(person.Name)
				if ent != nil {
					entityIDs[person.ID] = ent.ID
				}
				continue
			}
			log.Printf("  ✗ Failed to add person %s: %v", person.Name, err)
			continue
		}
		entityIDs[person.ID] = id
		totalEntities++
		fmt.Printf("  ✓ %s (id=%d)\n", person.Name, id)
	}

	// 4. Add Concepts
	fmt.Println("\n💡 Adding concepts...")
	for _, concept := range concepts {
		embedding := generateSemanticEmbedding(concept.Name+" "+concept.Description, vectorDim)
		id, err := c.AddEntity(concept.ID, concept.Name, "concept", concept.Description, embedding)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				ent, _ := c.GetEntityByTitle(concept.Name)
				if ent != nil {
					entityIDs[concept.ID] = ent.ID
				}
				continue
			}
			log.Printf("  ✗ Failed to add concept %s: %v", concept.Name, err)
			continue
		}
		entityIDs[concept.ID] = id
		totalEntities++
		fmt.Printf("  ✓ %s (id=%d)\n", concept.Name, id)
	}

	// 5. Add Regulations
	fmt.Println("\n📜 Adding regulations...")
	for _, reg := range regulations {
		embedding := generateSemanticEmbedding(reg.Name+" "+reg.Description, vectorDim)
		id, err := c.AddEntity(reg.ID, reg.Name, "regulation", reg.Description, embedding)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				ent, _ := c.GetEntityByTitle(reg.Name)
				if ent != nil {
					entityIDs[reg.ID] = ent.ID
				}
				continue
			}
			log.Printf("  ✗ Failed to add regulation %s: %v", reg.Name, err)
			continue
		}
		entityIDs[reg.ID] = id
		totalEntities++
		fmt.Printf("  ✓ %s (id=%d)\n", reg.Name, id)
	}

	// 6. Add Events
	fmt.Println("\n📅 Adding events...")
	for _, evt := range events {
		embedding := generateSemanticEmbedding(evt.Name+" "+evt.Description, vectorDim)
		id, err := c.AddEntity(evt.ID, evt.Name, "event", evt.Description, embedding)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				ent, _ := c.GetEntityByTitle(evt.Name)
				if ent != nil {
					entityIDs[evt.ID] = ent.ID
				}
				continue
			}
			log.Printf("  ✗ Failed to add event %s: %v", evt.Name, err)
			continue
		}
		entityIDs[evt.ID] = id
		totalEntities++
		fmt.Printf("  ✓ %s (id=%d)\n", evt.Name, id)
	}

	// 7. Add Relationships
	fmt.Println("\n🔗 Adding relationships...")
	for _, rel := range relationships {
		sourceID, ok1 := entityIDs[rel.SourceID]
		targetID, ok2 := entityIDs[rel.TargetID]
		if !ok1 || !ok2 {
			continue
		}

		_, err := c.AddRelationship("", sourceID, targetID, rel.RelType, rel.Description, rel.Weight)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				continue
			}
			log.Printf("  ✗ Failed to add relationship: %v", err)
			continue
		}
		totalRelationships++
	}
	fmt.Printf("  ✓ Added %d relationships\n", totalRelationships)

	// 8. Add Text Units
	fmt.Println("\n📝 Adding text units...")
	for _, tc := range textContents {
		docID, ok := docIDs[tc.DocID]
		if !ok {
			continue
		}

		embedding := generateSemanticEmbedding(tc.Content, vectorDim)
		_, err := c.AddTextUnit(tc.ChunkID, docID, tc.Content, embedding, len(tc.Content)/4)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				continue
			}
			log.Printf("  ✗ Failed to add text unit %s: %v", tc.ChunkID, err)
			continue
		}
		totalTextUnits++
	}
	fmt.Printf("  ✓ Added %d text units\n", totalTextUnits)

	// 9. Compute Communities
	fmt.Println("\n🧩 Computing communities (Leiden algorithm)...")
	communities, err := c.ComputeCommunities(1.0, 10)
	if err != nil {
		log.Printf("  ✗ Failed to compute communities: %v", err)
	} else {
		fmt.Printf("  ✓ Found %d communities\n", len(communities.Communities))
		for i, comm := range communities.Communities {
			if i >= 5 {
				fmt.Printf("  ... and %d more communities\n", len(communities.Communities)-5)
				break
			}
			fmt.Printf("    • %s (%d entities)\n", comm.Title, len(comm.EntityIDs))
		}
	}

	// 10. Try hierarchical Leiden
	fmt.Println("\n🏔️ Computing hierarchical communities...")
	hierarchical, err := c.HierarchicalLeiden(3, 1.0)
	if err != nil {
		log.Printf("  ✗ Failed to compute hierarchical communities: %v", err)
	} else {
		fmt.Printf("  ✓ Found %d total communities\n", hierarchical.TotalCommunities)
		for level, count := range hierarchical.LevelCounts {
			fmt.Printf("    • Level %d: %d communities\n", level, count)
		}
	}

	// Summary
	fmt.Println("\n───────────────────────────────────────────────────────────────")
	fmt.Println("📊 SEEDING SUMMARY")
	fmt.Printf("  • Entities:      %d\n", totalEntities)
	fmt.Printf("  • Relationships: %d\n", totalRelationships)
	fmt.Printf("  • Text Units:    %d\n", totalTextUnits)

	// Final stats
	info, err := c.Info()
	if err != nil {
		log.Fatalf("Info failed: %v", err)
	}
	fmt.Println("\n📈 CURRENT DATABASE STATE")
	fmt.Printf("  • Documents:     %d\n", info.DocumentCount)
	fmt.Printf("  • TextUnits:     %d\n", info.TextUnitCount)
	fmt.Printf("  • Entities:      %d\n", info.EntityCount)
	fmt.Printf("  • Relationships: %d\n", info.RelationshipCount)
	fmt.Printf("  • Communities:   %d\n", info.CommunityCount)
}

// =============================================================================
// Query Demos
// =============================================================================

func runQueryDemos(c *client.Client, vectorDim int) {
	fmt.Println()
	fmt.Println("🔍 RUNNING QUERY DEMOS")
	fmt.Println("═══════════════════════════════════════════════════════════════")

	queries := []struct {
		Name  string
		Query string
	}{
		{"Bank Indonesia Policy", "Bagaimana kebijakan moneter Bank Indonesia dan BI-Rate?"},
		{"Digital Payment", "Apa itu QRIS dan bagaimana perkembangan pembayaran digital di Indonesia?"},
		{"P2P Lending", "Bagaimana regulasi dan perkembangan fintech P2P lending di Indonesia?"},
		{"Banking Health", "Bagaimana kondisi kesehatan perbankan Indonesia dilihat dari NPL dan CAR?"},
		{"Stock Market", "Bagaimana kinerja IHSG dan saham perbankan di Bursa Efek Indonesia?"},
	}

	for i, q := range queries {
		fmt.Printf("\n🔎 Query %d: %s\n", i+1, q.Name)
		fmt.Printf("   \"%s\"\n", q.Query)
		fmt.Println("   ─────────────────────────────────────────────────────")

		embedding := generateSemanticEmbedding(q.Query, vectorDim)

		spec := types.QuerySpec{
			QueryVector:    embedding,
			SearchTypes:    []types.SearchType{types.SearchTypeTextUnit, types.SearchTypeEntity},
			TopK:           5,
			KHops:          2,
			MaxEntities:    20,
			MaxTextUnits:   5,
			MaxCommunities: 5,
			DeadlineMs:     100,
		}

		start := time.Now()
		result, err := c.Query(spec)
		duration := time.Since(start)

		if err != nil {
			fmt.Printf("   ✗ Query failed: %v\n", err)
			continue
		}

		fmt.Printf("   ⏱️  Query time: %v\n", duration)
		fmt.Printf("   📊 Results: %d text units, %d entities, %d relationships\n",
			len(result.TextUnits), len(result.Entities), len(result.Relationships))

		// Show top text units
		if len(result.TextUnits) > 0 {
			fmt.Println("\n   📄 Top Text Units:")
			for j, tu := range result.TextUnits {
				if j >= 2 {
					break
				}
				content := tu.TextUnit.Content
				if len(content) > 100 {
					content = content[:100] + "..."
				}
				fmt.Printf("      %d. [sim=%.3f] %s\n", j+1, tu.Similarity, content)
			}
		}

		// Show top entities
		if len(result.Entities) > 0 {
			fmt.Println("\n   🏷️  Top Entities:")
			for j, ent := range result.Entities {
				if j >= 5 {
					break
				}
				fmt.Printf("      %d. [sim=%.3f] %s (%s)\n", j+1, ent.Similarity, ent.Entity.Title, ent.Entity.Type)
			}
		}

		// Show relationships
		if len(result.Relationships) > 0 {
			fmt.Println("\n   🔗 Related Connections:")
			for j, rel := range result.Relationships {
				if j >= 3 {
					break
				}
				fmt.Printf("      • %s\n", rel.Relationship.Description)
			}
		}
	}

	// Explain demo
	fmt.Println()
	fmt.Println("📖 EXPLAIN DEMO")
	fmt.Println("───────────────────────────────────────────────────────────────")
	fmt.Println("   Running explain on last query...")

	info, err := c.Info()
	if err != nil {
		log.Fatalf("Info failed: %v", err)
	}
	if info.EntityCount > 0 {
		// Just show we ran explain on a recent query
		explain, err := c.Explain(1)
		if err != nil {
			fmt.Printf("   ✗ Explain failed: %v\n", err)
		} else {
			fmt.Printf("   ✓ Query had %d seed items\n", len(explain.Seeds))
			fmt.Printf("   ✓ Traversal depth: %d steps\n", len(explain.Traversal))
		}
	}
}

// =============================================================================
// Embedding Generation (Simulated)
// =============================================================================

// generateSemanticEmbedding creates a pseudo-semantic embedding for demo purposes.
// In production, you would use OpenAI, Cohere, or other embedding APIs.
func generateSemanticEmbedding(text string, dim int) []float32 {
	// Simple hash-based pseudo-embedding for consistent results
	embedding := make([]float32, dim)

	// Use text content to seed the embedding
	text = strings.ToLower(text)
	words := strings.Fields(text)

	// Create semantic clusters based on keywords
	financialWords := map[string]float32{
		"bank": 0.8, "perbankan": 0.8, "kredit": 0.7, "pinjaman": 0.7,
		"suku": 0.6, "bunga": 0.6, "bi": 0.9, "ojk": 0.8,
		"rupiah": 0.7, "idr": 0.7, "inflasi": 0.6,
	}

	digitalWords := map[string]float32{
		"digital": 0.8, "fintech": 0.9, "e-money": 0.8, "qris": 0.9,
		"gopay": 0.7, "ovo": 0.7, "dana": 0.7, "paylater": 0.8,
		"p2p": 0.8, "lending": 0.7, "online": 0.6,
	}

	marketWords := map[string]float32{
		"ihsg": 0.9, "saham": 0.8, "bei": 0.8, "bursa": 0.8,
		"investor": 0.7, "pasar": 0.6, "modal": 0.7,
		"obligasi": 0.6, "reksa": 0.6,
	}

	regulationWords := map[string]float32{
		"regulasi": 0.8, "peraturan": 0.8, "pojk": 0.9, "pbi": 0.9,
		"undang": 0.7, "hukum": 0.6, "izin": 0.6, "pengawasan": 0.7,
	}

	// Calculate cluster scores
	var financialScore, digitalScore, marketScore, regulationScore float32
	for _, word := range words {
		if score, ok := financialWords[word]; ok {
			financialScore += score
		}
		if score, ok := digitalWords[word]; ok {
			digitalScore += score
		}
		if score, ok := marketWords[word]; ok {
			marketScore += score
		}
		if score, ok := regulationWords[word]; ok {
			regulationScore += score
		}
	}

	// Normalize scores
	maxScore := max(financialScore, digitalScore, marketScore, regulationScore, 1.0)
	financialScore /= maxScore
	digitalScore /= maxScore
	marketScore /= maxScore
	regulationScore /= maxScore

	// Generate embedding with semantic structure
	seed := hashString(text)
	rng := rand.New(rand.NewSource(seed))

	// First quarter: financial features
	for i := 0; i < dim/4; i++ {
		embedding[i] = (rng.Float32() - 0.5) * 0.5
		if financialScore > 0.3 {
			embedding[i] += financialScore * 0.3
		}
	}

	// Second quarter: digital features
	for i := dim / 4; i < dim/2; i++ {
		embedding[i] = (rng.Float32() - 0.5) * 0.5
		if digitalScore > 0.3 {
			embedding[i] += digitalScore * 0.3
		}
	}

	// Third quarter: market features
	for i := dim / 2; i < 3*dim/4; i++ {
		embedding[i] = (rng.Float32() - 0.5) * 0.5
		if marketScore > 0.3 {
			embedding[i] += marketScore * 0.3
		}
	}

	// Fourth quarter: regulation features
	for i := 3 * dim / 4; i < dim; i++ {
		embedding[i] = (rng.Float32() - 0.5) * 0.5
		if regulationScore > 0.3 {
			embedding[i] += regulationScore * 0.3
		}
	}

	// Normalize to unit vector
	normalize(embedding)

	return embedding
}

func hashString(s string) int64 {
	var hash int64 = 5381
	for _, c := range s {
		hash = ((hash << 5) + hash) + int64(c)
	}
	return hash
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x * x)
	}
	norm := float32(math.Sqrt(sum))
	if norm > 0 {
		for i := range v {
			v[i] /= norm
		}
	}
}

func max(vals ...float32) float32 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
